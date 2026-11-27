package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// Parallel-implementation guard (F-0257): the Activity registry (F-0130/F-0208)
// already records who is on what, but nothing mechanical stopped a second
// session from implementing the same feature — `feature start` never consulted
// it (F-0253 shipped twice, one MR wasted). The guard runs at the point where
// the rework is born: entering development. Identity is the session binding
// (F-0151) first, actor second — `user@host` cannot tell two sessions on one
// machine apart, so an ambiguous match warns instead of blocking (a project
// without hooks must not get a false refusal on the normal F-0208 flow).

// ErrActiveWorkElsewhere is the sentinel for a start refused because another
// session or actor already has active work on the feature.
var ErrActiveWorkElsewhere = errors.New("another session is already working on this feature")

// parallelWork names one active work that collides with a start or an amend.
// Session is the newest other session bound to it ("" without session
// evidence, as in the start) and Signal the age of its last signal (F-0485).
// Card is the live child it was found on, for an amend ("" on the card itself).
type parallelWork struct {
	ID      string
	Actor   string
	Title   string
	Age     time.Duration
	Session string
	Signal  time.Duration
	Card    string
}

func (p parallelWork) line() string {
	s := "  " + p.ID
	if p.Card != "" {
		s += "  on " + p.Card
	}
	s += fmt.Sprintf("  actor: %s  age: %s", p.Actor, shortAge(p.Age))
	if p.Session != "" {
		s += fmt.Sprintf("  session: %s signal %s ago", p.Session, shortAge(p.Signal))
	}
	return s + "  " + p.Title
}

// shortAge renders a work's age the way `cerne work list` does.
func shortAge(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// myActors lists the identities this process may have registered work under:
// `work start` stamps "<user>@<host>" while the auto-work path prefers
// CERNE_ACTOR, so a single session can own works under either name — comparing
// against only one of them turns the agent's own pickup work into a foreign
// match and blocks the F-0208 flow.
func myActors() []string {
	actors := []string{defaultWorkActor()}
	if a := autoWorkActor(); a != actors[0] {
		actors = append(actors, a)
	}
	return actors
}

// classifyParallelWorks partitions the active works referencing featureID:
// foreign ones (provably another session, or another actor) refuse the start,
// ambiguous ones (one of my own actor identities, with no session evidence
// either way) only warn. Works a session of mine (main thread or subagent)
// holds are this session's own and never surface. sessions maps a work to the
// sessions bound to it (WorkSessions, newest signal first): a W- binding and a
// card binding after `feature start` alike (F-0485).
func classifyParallelWorks(works []store.Work, featureID string, mine []string, mySession string, sessions map[string][]SessionSignal, now time.Time) (foreign, ambiguous []parallelWork) {
	for _, w := range works {
		if w.State != domain.WorkActive || !slices.Contains(w.Refs, featureID) {
			continue
		}
		pw := toParallelWork(w, now)
		if mySession != "" {
			var other *SessionSignal
			mineHere := false
			for i, s := range sessions[w.ID] {
				if s.Session == mySession {
					mineHere = true
				} else if other == nil {
					other = &sessions[w.ID][i]
				}
			}
			if mineHere {
				continue // this session's own registration (F-0208 pickup)
			}
			if other != nil {
				pw.Session, pw.Signal = other.Session[:min(8, len(other.Session))], now.Sub(other.LastSignal)
				foreign = append(foreign, pw)
				continue
			}
		}
		// A pre-F-0350 work carries the bare "agent", shared by every agent on
		// every machine: it proves nothing either way, so it only warns.
		if !slices.Contains(mine, w.Actor) && w.Actor != legacyAgentActor {
			foreign = append(foreign, pw)
		} else {
			ambiguous = append(ambiguous, pw)
		}
	}
	return foreign, ambiguous
}

// boundElsewhere reports whether some session other than mySession holds the
// binding for work id. A subagent's <session>.<agent> key belongs to its
// session: a subagent of mine is not elsewhere (F-0376).
func boundElsewhere(bindings map[string]string, id, mySession string) bool {
	for key, bound := range bindings {
		if sess, _ := splitBindingName(key); bound == id && sess != mySession {
			return true
		}
	}
	return false
}

func toParallelWork(w store.Work, now time.Time) parallelWork {
	return parallelWork{ID: w.ID, Actor: w.Actor, Title: w.Title, Age: now.Sub(w.CreatedAt)}
}

// guardParallelStart enforces the guard for a feature entering development:
// without force a foreign active work refuses the transition (nothing is
// written); with force it passes reporting forced, so the caller marks the
// override on the event. Ambiguous matches come back as warnings.
func guardParallelStart(repoDir, projDir, featureID string, force bool) (forced bool, warnings []string, err error) {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return false, nil, err
	}
	// ponytail: session evidence stays off. Since F-0325 every agent session
	// exports CERNE_SESSION, and a binding file outlives its session — the
	// same person after /clear or a restart has a new id, so feeding the
	// evidence would turn the F-0208 ambiguous warning into a hard refusal
	// (critic C14). Turn it back on when sessions carry liveness.
	foreign, ambiguous := classifyParallelWorks(works, featureID, myActors(), "", nil, time.Now().UTC())
	for _, p := range ambiguous {
		warnings = append(warnings, fmt.Sprintf("warning: %s is active on %s (same actor, session unknown) — confirm nobody else is implementing it", p.ID, featureID))
	}
	if len(foreign) == 0 {
		return false, warnings, nil
	}
	if force {
		return true, warnings, nil
	}
	return false, nil, fmt.Errorf("%w (%s):\n%s\nstop and confirm with them before implementing; use --force only if you are taking it over", ErrActiveWorkElsewhere, featureID, workLines(foreign))
}

// liveChildren is id's direct children still in flight (candidate, approved,
// in development), sorted by id: cards decomposed from its current text.
func liveChildren(features []store.Feature, id string) []store.Feature {
	var out []store.Feature
	for _, c := range store.ChildrenByParent(features)[id] {
		if c.State == domain.StateCandidate || c.State == domain.StateApproved || c.State == domain.StateInDevelopment {
			out = append(out, c)
		}
	}
	return out
}

// classifyAmendWorks is classifyParallelWorks over the card id and each of
// its live children, each work once (F-0485): in the case that motivated it,
// the other session's work was on the child task, not on the epic.
func classifyAmendWorks(works []store.Work, features []store.Feature, id string, mine []string, mySession string, sessions map[string][]SessionSignal, now time.Time) (foreign, ambiguous []parallelWork) {
	cards := []string{id}
	for _, c := range liveChildren(features, id) {
		cards = append(cards, c.ID)
	}
	seen := map[string]bool{}
	for _, card := range cards {
		fs, as := classifyParallelWorks(works, card, mine, mySession, sessions, now)
		for _, group := range []struct {
			in  []parallelWork
			out *[]parallelWork
		}{{fs, &foreign}, {as, &ambiguous}} {
			for _, p := range group.in {
				if seen[p.ID] {
					continue
				}
				seen[p.ID] = true
				if card != id {
					p.Card = card
				}
				*group.out = append(*group.out, p)
			}
		}
	}
	return foreign, ambiguous
}

// guardAmend is the session guard of a spec or plan amend on f (F-0485): the
// start's guard with session evidence on — the amend is rare, so a refusal
// after /clear costs one --force, while two agent sessions on one machine
// share the actor and only the session tells them apart. Without force a
// foreign work refuses, naming who is on the card, what the amend causes in
// f's state and what was written against the current text; nothing is
// written. With force the crossed works' ids come back for the event, their
// lines in notes. Same-actor works with no session evidence only warn.
func guardAmend(repoDir string, f store.Feature, doc string, force bool) (crossed, notes []string, err error) {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return nil, nil, err
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return nil, nil, err
	}
	sess := CurrentSession() // F-0525: also recognizes CURSOR_CONVERSATION_ID in Cursor shells
	sessions := WorkSessions(works, SessionSignals(filepath.Dir(repoDir)))
	foreign, ambiguous := classifyAmendWorks(works, features, f.ID, myActors(), sess, sessions, time.Now().UTC())
	why := "same actor, no session bound on this machine"
	if sess == "" {
		why = "same actor; no session identity (CERNE_SESSION or CURSOR_CONVERSATION_ID), so sessions are not compared"
	}
	for _, p := range ambiguous {
		notes = append(notes, fmt.Sprintf("warning: %s is active on %s (%s) — confirm nobody else is working on it", p.ID, cmpOr(p.Card, f.ID), why))
	}
	var against []string
	if plan, _ := os.ReadFile(FeatureDocPath(repoDir, f.ID, "plan.md")); doc == "spec" && doccheck.HasRealContent(plan) {
		against = append(against, "plan.md")
	}
	for _, c := range liveChildren(features, f.ID) {
		against = append(against, fmt.Sprintf("%s (%s)", c.ID, c.State))
	}
	effect := string(f.State) + ": the " + doc + " amend takes effect at once, with no re-approval"
	if f.State == domain.StateApproved && doc == "spec" {
		effect = "approved: a spec that differs from the approved one stales the approval — 'cerne feature start' refuses until the owner re-approves"
	}
	if len(against) > 0 {
		effect += "\nwritten against the current " + doc + ": " + strings.Join(against, ", ")
	}
	if len(foreign) > 0 && !force {
		return nil, nil, fmt.Errorf("%w (%s or its live children):\n%s\n%s\nstop and confirm with them before amending — an old signal can mean that session ended; use --force to amend anyway", ErrActiveWorkElsewhere, f.ID, workLines(foreign), effect)
	}
	if len(foreign) > 0 {
		notes = append(notes, "forced past active work of another session or actor:\n"+workLines(foreign))
	}
	for _, p := range foreign {
		crossed = append(crossed, p.ID)
	}
	if f.State == domain.StateInDevelopment {
		notes = append(notes, strings.Replace(effect, "the current", "the previous", 1))
	}
	return crossed, notes, nil
}

// workLines renders one line per work, joined.
func workLines(ps []parallelWork) string {
	lines := make([]string, len(ps))
	for i, p := range ps {
		lines[i] = p.line()
	}
	return strings.Join(lines, "\n")
}

// sessionBindingsIn reads the session→entity bindings under cacheDir as is
// (no liveness check), keyed by file name — <session> or <session>.<agent>
// (F-0376); nil when there are none.
func sessionBindingsIn(cacheDir string) map[string]string {
	entries, err := os.ReadDir(filepath.Join(cacheDir, sessionsSubdir))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(cacheDir, sessionsSubdir, e.Name()))
		if err != nil {
			continue
		}
		out[e.Name()] = strings.TrimSpace(string(raw))
	}
	return out
}

// boundHere reports whether session sess — its main thread or one of its
// subagents — holds the binding for work id (F-0376).
func boundHere(bindings map[string]string, id, sess string) bool {
	for key, bound := range bindings {
		if s, _ := splitBindingName(key); bound == id && sess != "" && s == sess {
			return true
		}
	}
	return false
}
