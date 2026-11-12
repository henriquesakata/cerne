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
	"github.com/henriquesakata/cerne/internal/store"
)

// WorkOutcome reports a work transition that was applied and synced.
type WorkOutcome struct {
	ID   string
	From domain.WorkState
	To   domain.WorkState
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
}

// StartWork mints a new active work (agent activity, F-0130) in the backend
// clone at repoDir: validates each ref (F-/D- id) exists, allocates the next
// W- id, persists the entity, appends the work.started event and commits+
// pushes the backend, refreshing the docs.lock snapshot in projDir. actor is
// agent when non-empty, else "<os user>@<hostname>" — the default identity of
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
	actor := strings.TrimSpace(agent)
	if actor == "" {
		actor = defaultWorkActor()
	}
	paths = cleanWorkPaths(paths)
	var out StartedWork
	var fromTitle string
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		cleaned, err := validateWorkRefs(repoDir, refs)
		if err != nil {
			return "", err
		}
		// Duplicate-registration guard (F-0147): someone may already be on
		// this. The auto-registered works (F-0146) never pass through here —
		// they are idempotent by construction.
		var origin store.Work
		if from != "" {
			if origin, err = store.LoadWork(repoDir, strings.ToUpper(strings.TrimSpace(from))); errors.Is(err, store.ErrNotFound) {
				return "", fmt.Errorf("--from: activity %s not found in the backend", from)
			} else if err != nil {
				return "", err
			}
			// Every comparison below uses the stored id: on a case-insensitive
			// filesystem the raw one may have loaded a differently-cased file.
			from = origin.ID
			if origin.State != domain.WorkActive {
				return "", fmt.Errorf("--from: activity %s is %s, not active", from, origin.State)
			}
			if len(cleaned) == 0 {
				cleaned = origin.Refs
			}
		}
		forced, err := guardRegistration(repoDir, title, force, from)
		if err != nil {
			return "", err
		}
		prior, err := store.LoadAllWorks(repoDir)
		if err != nil {
			return "", err
		}
		// The origin is closed by --from (crit. 8), never also by F-0341.
		prior = slices.DeleteFunc(prior, func(w store.Work) bool { return from != "" && w.ID == from })
		var handoffs []WorkHandoff
		if e, ok, err := lastHandoff(repoDir, origin); err != nil {
			return "", err
		} else if ok {
			handoffs = append(handoffs, WorkHandoff{ID: from, Entry: e})
		}
		crossing, err := HandoffsFor(repoDir, cleaned, from)
		if err != nil {
			return "", err
		}
		handoffs = append(handoffs, crossing...)
		id, err := store.NextID(repoDir, "W")
		if err != nil {
			return "", err
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
			return "", err
		}
		payload := map[string]string{"title": title, "actor": actor}
		if forced {
			payload["guard"] = "forced"
		}
		ev := domain.NewEvent(id, "work.started", payload, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		out = StartedWork{Work: w, From: from, Handoffs: handoffs}
		fromTitle = origin.Title
		if from != "" {
			if err := closeWork(repoDir, origin, domain.ActionComplete, "handed off to "+id); err != nil {
				return "", err
			}
		}
		if activityAutoEnabled(projDir) {
			if out.Superseded, out.Kept, out.Held, err = supersedePlanning(repoDir, prior, cleaned, id); err != nil {
				return "", err
			}
		}
		// A work F-0341 closed in this very call cannot be taken over with
		// --from (it would be refused): its handoff comes without the hint.
		for i := range out.Handoffs {
			if slices.Contains(out.Superseded, out.Handoffs[i].ID) {
				out.Handoffs[i].Hint = false
			}
		}
		return fmt.Sprintf("cerne: start %s (%s)", id, title), nil
	})
	if err != nil {
		return StartedWork{}, err
	}
	if out.From != "" { // the origin closed like `work done` would have (F-0339)
		dispatchWebhook(projDir, out.From, "work", string(domain.ActionComplete), string(domain.WorkActive), string(domain.WorkDone), fromTitle)
	}
	dispatchWebhook(projDir, out.ID, "work", "start", "", string(out.State), out.Title)
	return out, nil
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
	sess := os.Getenv("CERNE_SESSION")
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
			if bindings[sess] != w.ID {
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
// refreshing the docs.lock snapshot in projDir.
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
	if err != nil {
		return WorkOutcome{}, err
	}
	dispatchWebhook(projDir, id, "work", actionName, string(out.From), string(out.To), title)
	return out, nil
}

// defaultWorkActor returns "<user>@<hostname>" for the current process, best
// effort: an empty/unavailable piece is simply omitted rather than failing
// StartWork over an identity nicety.
func defaultWorkActor() string {
	host, _ := os.Hostname()
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
// target: it must be a feature (F-) or decision (D-) that exists in the
// backend, so no dangling ref is stored. Returns the cleaned list (nil when
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
		default:
			return nil, fmt.Errorf("--ref: %q must be a feature (F-) or decision (D-)", id)
		}
		out = append(out, id)
	}
	return out, nil
}
