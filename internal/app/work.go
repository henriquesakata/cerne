package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// WorkOutcome reports a work transition that was applied and synced.
type WorkOutcome struct {
	ID   string
	From domain.WorkState
	To   domain.WorkState
	// AlreadyDone: a done request on a work that was already done — nothing written.
	AlreadyDone bool
}

// StartedWork is what StartWork did: the new work, plus the planning works of
// this process's actors it closed as superseded (F-0341 crit. 4), the
// unbound ones it kept active (crit. 5) and the ones another session holds —
// possibly live work, so never offered for closing.
type StartedWork struct {
	store.Work
	Superseded []string
	Kept       []string
	Held       []string
	From       string        // the canonical id of the --from origin ("" without it)
	Handoffs   []WorkHandoff // what to read before starting (F-0339): the --from origin's, then the crossing refs'
	Taken      store.Handoff // the handoff this start took (F-0368); zero ID when none
}

// StartWork mints a new active work (agent activity, F-0130) in the backend
// clone at repoDir: validates each ref (F-/D- id) exists, allocates the next
// W- id, persists the entity, appends the work.started event and commits+
// pushes the backend, refreshing the docs.lock snapshot in projDir. actor is
// agent when non-empty, else the machine pseudonym (F-0442) — the default identity of
// whoever/whatever is running this activity. In the same commit, the new work
// supersedes the same actor's planning works on its features (F-0341).
// from (F-0339) is an active work this one takes over: its refs are inherited
// when none are given, the duplicate guard ignores it, and it closes as done
// ("handed off to <new>") in the same commit — F-0341 never also evaluates it.
func StartWork(repoDir, projDir, title string, refs []string, agent string, force bool, paths []string, from string) (StartedWork, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return StartedWork{}, fmt.Errorf("the activity description cannot be empty")
	}
	actor := domain.ActorLabel(strings.TrimSpace(agent))
	if actor == "" {
		actor = resolveActor(defaultWorkActor()) // F-0442: the one actor rule
	}
	var out StartedWork
	var fromTitle string
	err := MutateAndSnapshot(repoDir, projDir, func() (msg string, err error) {
		out, fromTitle, msg, err = startWorkTx(repoDir, projDir, startParams{
			title: title, refs: refs, actor: actor, force: force, paths: cleanWorkPaths(paths),
			from: from, sess: CurrentSession(), // F-0525: also recognizes CURSOR_CONVERSATION_ID
		})
		return msg, err
	})
	if err != nil {
		return StartedWork{}, err
	}
	dispatchStarted(projDir, out, fromTitle)
	return out, nil
}

// startParams is what startWorkTx mints a work from. from is an active origin
// to close; ho is the pending handoff the start takes (F-0368) — StartWork
// finds the origin's own, TakeHandoff passes it even when the origin is no
// longer active (from is then ""). sess and note are recorded on the taken HO.
type startParams struct {
	title, actor string
	refs, paths  []string
	force        bool
	finalRefs    bool // refs are final: never inherit the origin's (TakeHandoff dropped the dangling ones)
	from         string
	ho           *store.Handoff
	sess, note   string
}

// startWorkTx is StartWork's mutation, run inside a MutateAndSnapshot. It
// returns the started work, the origin's title (for its webhook) and the
// commit message.
func startWorkTx(repoDir, projDir string, p startParams) (StartedWork, string, string, error) {
	title, actor, from, force, paths := p.title, p.actor, p.from, p.force, p.paths
	cleaned, err := validateWorkRefs(repoDir, p.refs)
	if err != nil {
		return StartedWork{}, "", "", err
	}
	// Duplicate-registration guard (F-0147): someone may already be on
	// this. The auto-registered works (F-0146) never pass through here —
	// they are idempotent by construction.
	var origin store.Work
	if from != "" {
		if origin, err = store.LoadWork(repoDir, strings.ToUpper(strings.TrimSpace(from))); errors.Is(err, store.ErrNotFound) {
			return StartedWork{}, "", "", fmt.Errorf("--from: activity %s not found in the backend", from)
		} else if err != nil {
			return StartedWork{}, "", "", err
		}
		// Every comparison below uses the stored id: on a case-insensitive
		// filesystem the raw one may have loaded a differently-cased file.
		from = origin.ID
		if origin.State != domain.WorkActive {
			return StartedWork{}, "", "", fmt.Errorf("--from: activity %s is %s, not active", from, origin.State)
		}
		// Owner's principle (F-0341): live work of another session is not
		// closed on a guess. A handoff is the proof the owner let go; a
		// typo like W-0043 has none, and another session holds the binding.
		if origin.HandoffAt == nil && !force && boundElsewhere(sessionBindingsIn(filepath.Dir(repoDir)), from, CurrentSession()) { // F-0525
			return StartedWork{}, "", "", fmt.Errorf("--from: %s has no handoff and is bound to another session — it may be live work; confirm with that session, or use --force to take it over anyway", from)
		}
		if len(cleaned) == 0 && !p.finalRefs {
			cleaned = origin.Refs
		}
		// F-0368 crit. 6: taking a work over takes its pending handoff.
		if p.ho == nil {
			if h, ok, err := pendingHandoffOf(repoDir, from); err != nil {
				return StartedWork{}, "", "", err
			} else if ok {
				p.ho = &h
			}
		}
	}
	forced, err := guardRegistration(repoDir, title, force, append([]string{from}, cleaned...)...)
	if err != nil {
		return StartedWork{}, "", "", err
	}
	prior, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return StartedWork{}, "", "", err
	}
	// The origin is closed by --from (crit. 8), never also by F-0341.
	prior = slices.DeleteFunc(prior, func(w store.Work) bool { return from != "" && w.ID == from })
	var handoffs []WorkHandoff
	if e, ho, ok, err := lastHandoff(repoDir, origin); err != nil {
		return StartedWork{}, "", "", err
	} else if ok {
		handoffs = append(handoffs, WorkHandoff{ID: from, Entry: e, HO: ho})
	}
	crossing, err := HandoffsFor(repoDir, cleaned, from)
	if err != nil {
		return StartedWork{}, "", "", err
	}
	handoffs = append(handoffs, crossing...)
	id, err := store.NextID(repoDir, "W")
	if err != nil {
		return StartedWork{}, "", "", err
	}
	now := time.Now().UTC()
	w := store.Work{
		ID:        id,
		Title:     title,
		State:     domain.WorkActive,
		Actor:     actor,
		Refs:      cleaned,
		Paths:     paths,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SaveWork(repoDir, w); err != nil {
		return StartedWork{}, "", "", err
	}
	payload := map[string]string{"title": title, "actor": actor}
	if forced {
		payload["guard"] = "forced"
	}
	ev := domain.NewEvent(id, "work.started", payload, EventActor())
	if err := store.AppendEvent(repoDir, ev); err != nil {
		return StartedWork{}, "", "", err
	}
	out := StartedWork{Work: w, From: from, Handoffs: handoffs}
	if from != "" {
		// An explicit handoff, closed like `work done`: ends the run, no via=auto.
		note := "handed off to " + id
		if p.ho != nil {
			note += " via " + p.ho.ID
		}
		if _, err := closeWorkEntity(repoDir, origin, domain.ActionComplete, note, map[string]string{"note": note}); err != nil {
			return StartedWork{}, "", "", err
		}
	}
	if p.ho != nil {
		// taken_by is the new W-'s actor (F-0368 crit. 16).
		if out.Taken, err = markTaken(repoDir, *p.ho, actor, id, p.sess, p.note, now); err != nil {
			return StartedWork{}, "", "", err
		}
	}
	if activityAutoEnabled(projDir) {
		if out.Superseded, out.Kept, out.Held, err = supersedePlanning(repoDir, prior, cleaned, id); err != nil {
			return StartedWork{}, "", "", err
		}
	}
	// A work F-0341 closed in this very call cannot be taken over with
	// --from (it would be refused): its handoff comes without the hint.
	for i := range out.Handoffs {
		if slices.Contains(out.Superseded, out.Handoffs[i].ID) {
			out.Handoffs[i].Hint = false
		}
	}
	return out, origin.Title, fmt.Sprintf("cerne: start %s (%s)", id, title), nil
}

// dispatchStarted fires the webhooks of a committed start: the origin closed
// like `work done` would have (F-0339), then the new work.
func dispatchStarted(projDir string, out StartedWork, fromTitle string) {
	if out.From != "" {
		dispatchWebhook(projDir, out.From, "work", string(domain.ActionComplete), string(domain.WorkActive), string(domain.WorkDone), fromTitle)
	}
	dispatchWebhook(projDir, out.ID, "work", "start", "", string(out.State), out.Title)
}

// supersedePlanning (F-0341 crit. 4/5, owner's decision of 2026-11-11)
// closes, as superseded by newID, the planning work of this process's actors
// on each feature in refs that this very session is bound to (F-0151): the
// session re-registering. Every other one of theirs stays, to be warned
// about — in held when another session is bound to it, else (unbound, or met
// with no CERNE_SESSION) in kept: the agent actor is the same on every
// machine, another machine's binding is not visible here, and "unbound" does
// not prove the session ended. works lists the board before newID existed.
func supersedePlanning(repoDir string, works []store.Work, refs []string, newID string) (closed, kept, held []string, err error) {
	mine := myActors()
	bindings := sessionBindingsIn(filepath.Dir(repoDir))
	sess := CurrentSession() // F-0525: also recognizes CURSOR_CONVERSATION_ID
	for _, ref := range refs {
		if !domain.IsFeatureID(ref) {
			continue
		}
		f, err := store.LoadFeature(repoDir, ref)
		if err != nil {
			return nil, nil, nil, err
		}
		planning, err := planningWorks(repoDir, works, ref, f.State)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, w := range planning {
			if !slices.Contains(mine, w.Actor) {
				continue
			}
			if !boundHere(bindings, w.ID, sess) {
				if boundElsewhere(bindings, w.ID, sess) {
					held = append(held, w.ID)
				} else {
					kept = append(kept, w.ID)
				}
				continue
			}
			if err := closeWork(repoDir, w, domain.ActionComplete, "superseded by "+newID); err != nil {
				return nil, nil, nil, err
			}
			closed = append(closed, w.ID)
		}
	}
	return closed, kept, held, nil
}

// FinishWork ends work id in the backend clone at repoDir: active -> done
// (note optional, the outcome of the activity) when abandon is false, or
// active -> abandoned (reason required) when abandon is true. A terminal work
// refuses with domain.ErrInvalidTransition — the record stays as history.
// Appends the matching work.done/work.abandoned event and commits+pushes,
// refreshing the docs.lock snapshot in projDir. A done request on a work that
// is already done is a no-op (AlreadyDone, nothing written); abandoned stays refused.
func FinishWork(repoDir, projDir, id, note string, abandon bool, reason string) (WorkOutcome, error) {
	if abandon && strings.TrimSpace(reason) == "" {
		return WorkOutcome{}, fmt.Errorf("--reason is required to abandon")
	}
	var out WorkOutcome
	var title, actionName string
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		w, err := store.LoadWork(repoDir, id)
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("activity %s not found in the backend", id)
		}
		if err != nil {
			return "", err
		}

		if !abandon && w.State == domain.WorkDone {
			out = WorkOutcome{ID: id, From: w.State, To: w.State, AlreadyDone: true}
			return "", nil // idempotent (B-0057): writes nothing, surfaces as ErrNothingToCommit
		}
		action := domain.ActionComplete
		closingNote := strings.TrimSpace(note)
		if abandon {
			action = domain.ActionAbandon
			closingNote = strings.TrimSpace(reason)
		}
		payload := map[string]string{}
		if abandon {
			payload["reason"] = closingNote
		} else if closingNote != "" {
			payload["note"] = closingNote
		}
		oldState := w.State
		newState, err := closeWorkEntity(repoDir, w, action, closingNote, payload)
		if err != nil {
			return "", err
		}
		title = w.Title
		actionName = string(action)
		out = WorkOutcome{ID: id, From: oldState, To: newState}
		return fmt.Sprintf("cerne: finish %s", id), nil
	})
	if out.AlreadyDone && errors.Is(err, gitback.ErrNothingToCommit) {
		return out, nil
	}
	if err != nil {
		return WorkOutcome{}, err
	}
	dispatchWebhook(projDir, id, "work", actionName, string(out.From), string(out.To), title)
	return out, nil
}

// hostname is os.Hostname, replaceable so tests pin the machine.
var hostname = os.Hostname

// resolveActor is the one actor function (F-0350): CERNE_ACTOR="agent" becomes
// "agent:m-<8hex>" (the machine pseudonym, derived at run time, never written
// to the committed settings.json), any other explicit value goes through
// domain.ActorLabel (F-0442: nothing with an "@" is recorded), and unset yields
// fallback.
func resolveActor(fallback string) string {
	switch a := actorEnv(); a {
	case "":
		return fallback
	case legacyAgentActor:
		return agentActorPrefix + defaultWorkActor()
	default:
		return domain.ActorLabel(a)
	}
}

const (
	legacyAgentActor = "agent"
	agentActorPrefix = "agent:"
)

// actorEnv is the actor the environment declares: a non-empty CERNE_ACTOR
// (so CERNE_ACTOR=human always declares the human), or the legacy "agent" in a
// Claude Code, Cursor or Copilot CLI agent shell (B-0030, F-0428, F-0444).
// Cursor does not apply the settings.json env block that stamps CERNE_ACTOR,
// but puts CURSOR_AGENT=1 on its agent's terminals (seen in Cursor 3.25.6,
// undocumented — if it goes, the actor is simply the human default again).
func actorEnv() string {
	if a := os.Getenv("CERNE_ACTOR"); a != "" {
		return a
	}
	// Claude Code exports CLAUDECODE=1 in every shell it runs (seen in Claude
	// Code's Bash tool, 2026-11, B-0030): without `cerne hook install` the env
	// block that stamps CERNE_ACTOR is missing. Copilot CLI marks its bash
	// tool's shell with COPILOT_CLI=1 (probed on 1.0.112, F-0444); the hooks'
	// env block does not reach that shell. Codex exports CODEX_THREAD_ID (a
	// thread id, so any non-empty value) in its tool shells, no project trust or
	// hook install needed (codex-cli 0.169.0 `codex exec` and the interactive
	// Codex, 2026-11-20, B-0051).
	if os.Getenv("CLAUDECODE") == "1" || os.Getenv("CURSOR_AGENT") == "1" || os.Getenv("COPILOT_CLI") == "1" || os.Getenv("CODEX_THREAD_ID") != "" {
		return legacyAgentActor
	}
	return ""
}

// isAgentActor reports an agent identity: the F-0350 form or the legacy bare "agent".
func isAgentActor(a string) bool {
	return a == legacyAgentActor || strings.HasPrefix(a, agentActorPrefix)
}

// defaultWorkActor returns the machine pseudonym of "<user>@<hostname>" for the
// current process (F-0442), best effort: an empty/unavailable piece is simply
// omitted rather than failing StartWork over an identity nicety.
func defaultWorkActor() string {
	return domain.MachinePseudonym(rawUserAtHost())
}

func rawUserAtHost() string {
	host, _ := hostname()
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("USERNAME")
	}
	switch {
	case user != "" && host != "":
		return user + "@" + host
	case user != "":
		return user
	case host != "":
		return host
	default:
		return "unknown"
	}
}

// cleanWorkPaths trims and slash-normalizes the declared path globs (F-0151),
// dropping empties; nil when nothing survives.
func cleanWorkPaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// validateWorkRefs trims, dedupes (preserving order) and checks each ref
// target: it must be a feature-shaped (F-, E-, T-, B-), decision (D-), work
// (W-), note (N-) or runbook (RB-) id that exists in the backend, so no
// dangling ref is stored. Returns the cleaned list (nil when
// empty).
func validateWorkRefs(repoDir string, refs []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, raw := range refs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		switch {
		case domain.IsFeatureID(id):
			if _, err := store.LoadFeature(repoDir, id); errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("--ref: feature %s not found in the backend", id)
			} else if err != nil {
				return nil, err
			}
		case strings.HasPrefix(id, "D-"):
			if _, err := store.LoadDecision(repoDir, id); errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("--ref: decision %s not found in the backend", id)
			} else if err != nil {
				return nil, err
			}
		case strings.HasPrefix(id, "W-"), strings.HasPrefix(id, "N-"), strings.HasPrefix(id, "RB-"):
			if !store.EntityExists(repoDir, id) {
				return nil, fmt.Errorf("--ref: %s %s not found in the backend", map[string]string{"W": "work", "N": "note", "RB": "runbook"}[id[:strings.Index(id, "-")]], id)
			}
		default:
			return nil, fmt.Errorf("--ref: %q must be a feature (F-, E-, T-, B-), decision (D-), work (W-), note (N-) or runbook (RB-) id", id)
		}
		out = append(out, id)
	}
	return out, nil
}
