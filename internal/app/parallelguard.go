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
type parallelWork struct {
	ID      string
	Actor   string
	Title   string
	Age     time.Duration
	Session string
	Signal  time.Duration
}

func (p parallelWork) line() string {
	s := fmt.Sprintf("  %s  actor: %s  age: %s", p.ID, p.Actor, shortAge(p.Age))
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
	lines := make([]string, 0, len(foreign))
	for _, p := range foreign {
		lines = append(lines, p.line())
	}
	return false, nil, fmt.Errorf("%w (%s):\n%s\nstop and confirm with them before implementing; use --force only if you are taking it over", ErrActiveWorkElsewhere, featureID, strings.Join(lines, "\n"))
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
