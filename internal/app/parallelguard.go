package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
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

// parallelWork names one active work that collides with a start.
type parallelWork struct {
	ID    string
	Actor string
	Title string
	Age   time.Duration
}

func (p parallelWork) line() string {
	return fmt.Sprintf("  %s  actor: %s  age: %s  %s", p.ID, p.Actor, shortAge(p.Age), p.Title)
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
// either way) only warn. Works bound to mySession are this session's own and
// never surface.
func classifyParallelWorks(works []store.Work, featureID string, mine []string, mySession string, bindings map[string]string, now time.Time) (foreign, ambiguous []parallelWork) {
	for _, w := range works {
		if w.State != domain.WorkActive || !slices.Contains(w.Refs, featureID) {
			continue
		}
		if mySession != "" {
			if bindings[mySession] == w.ID {
				continue // this session's own registration (F-0208 pickup)
			}
			if boundElsewhere(bindings, w.ID, mySession) {
				foreign = append(foreign, toParallelWork(w, now))
				continue
			}
		}
		pw := toParallelWork(w, now)
		if !slices.Contains(mine, w.Actor) {
			foreign = append(foreign, pw)
		} else {
			ambiguous = append(ambiguous, pw)
		}
	}
	return foreign, ambiguous
}

// boundElsewhere reports whether some session other than mySession holds the
// binding for work id.
func boundElsewhere(bindings map[string]string, id, mySession string) bool {
	for sess, bound := range bindings {
		if bound == id && sess != mySession {
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
	sess := os.Getenv("CERNE_SESSION")
	foreign, ambiguous := classifyParallelWorks(works, featureID, myActors(), sess, sessionBindings(projDir), time.Now().UTC())
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

// sessionBindings reads every session→entity binding for the project as a map
// (session id → bound id). Best effort: an unresolvable cache dir or an
// unreadable sessions dir means no session evidence, which degrades the guard
// to the actor comparison rather than failing the transition.
func sessionBindings(projDir string) map[string]string {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		return nil
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return nil
	}
	return sessionBindingsIn(cacheDir)
}

// sessionBindingsIn reads the session→entity bindings under cacheDir as is
// (no liveness check); nil when there are none.
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
