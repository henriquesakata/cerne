package app

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// Auto-registered activity (F-0146): the feature transitions themselves record
// who is working on what — `start` opens an active work referencing the
// feature, and leaving in_development (complete/supersede) closes it — so the
// Activity view reflects in-flight implementations with zero extra agent
// discipline. Everything here runs inside the transition's critical section,
// landing in the same commit as the transition itself.

// activityAutoEnabled reads the [activity] auto switch from the project's
// .cernerc; a missing or unreadable config means the default (enabled).
func activityAutoEnabled(projDir string) bool {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		return true
	}
	return cfg.Activity.AutoEnabled()
}

// autoWorkActor identifies who the auto-work belongs to: the CERNE_ACTOR env
// marker when set (agent sessions, F-0131), else "<user>@<hostname>".
func autoWorkActor() string { return resolveActor(defaultWorkActor()) }

// openAutoWork creates the auto work for a feature entering development,
// unless the same actor already has an active work referencing it (a manual
// `work start --ref` or a previous auto-open) — re-registering would be noise.
func openAutoWork(repoDir string, f store.Feature) error {
	actor := autoWorkActor()
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return err
	}
	for _, w := range works {
		if w.State == domain.WorkActive && w.Actor == actor && slices.Contains(w.Refs, f.ID) {
			return nil
		}
	}
	id, err := store.NextID(repoDir, "W")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	w := store.Work{
		ID:        id,
		Title:     f.Title,
		State:     domain.WorkActive,
		Actor:     actor,
		Refs:      []string{f.ID},
		Auto:      true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SaveWork(repoDir, w); err != nil {
		return err
	}
	ev := domain.NewEvent(id, "work.started", map[string]string{"title": f.Title, "actor": actor, "via": "auto"}, EventActor())
	return store.AppendEvent(repoDir, ev)
}

// OpenAutoWorkForPush (F-0208 criterion 1) opens the pickup auto-work when the
// pushed batch includes a plan.md belonging to a feature still `approved`: the
// agent's first CLI touch on the backend after claiming the feature registers
// presence, closing the blind window before `cerne feature start` (whose gate
// requires a real plan and so lags the actual pickup by minutes to hours). Runs
// inside the same reconcile lock as the push (via ReconcileCommitPushMutate),
// so the auto-work lands in the same commit. Reuses openAutoWork's dedup: a
// prior manual or auto work by the same actor on the feature suppresses it.
func OpenAutoWorkForPush(repoDir, projDir string, changes []FileChange) error {
	if !activityAutoEnabled(projDir) {
		return nil
	}
	for _, ch := range changes {
		if ch.Deleted || path.Base(ch.Path) != "plan.md" {
			continue
		}
		id := path.Base(path.Dir(ch.Path))
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			continue // not a feature doc, or feature unreadable: nothing to register
		}
		if f.State != domain.StateApproved {
			continue
		}
		if err := openAutoWork(repoDir, f); err != nil {
			return err
		}
	}
	return nil
}

// closeAutoWorks ends every still-active AUTO work referencing the feature
// that just left in_development: done on complete, abandoned on supersede.
// Manual works (Auto=false) are never touched — closing those is the owner's
// call (the stale-work lint nags about forgotten ones).
func closeAutoWorks(repoDir, featureID string, action domain.Action) error {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return err
	}
	mine := myActors()
	bindings := sessionBindingsIn(filepath.Dir(repoDir))
	sess := os.Getenv("CERNE_SESSION")
	for _, w := range works {
		if w.State != domain.WorkActive {
			continue
		}
		// F-0208 criterion 2: a manual pickup work (single ref, same actor) is
		// adopted here too, so a work opened by openAutoWorkForPush never
		// outlives the feature it registered — only single-ref works of the
		// SAME actor qualify; a broader-scoped or another actor's work is left
		// for its owner (stale-work lint still nags a forgotten one). A work
		// another session is bound to (F-0151) is that session's and never
		// adopted. A work under this process's OTHER identity (F-0341 crit. 3,
		// the owner's decision of 2026-11-11) is adopted only when bound to
		// this very session: unbound proves nothing — a human's terminal
		// carries the same <user>@<host>.
		isAuto := w.Auto
		ownIdentity := w.Actor == autoWorkActor() || (slices.Contains(mine, w.Actor) && bindings[sess] == w.ID)
		isAdoptedManual := !w.Auto && ownIdentity && len(w.Refs) == 1 && w.Refs[0] == featureID &&
			!boundElsewhere(bindings, w.ID, sess)
		if (!isAuto && !isAdoptedManual) || !slices.Contains(w.Refs, featureID) {
			continue
		}
		workAction := domain.ActionComplete
		if action != domain.ActionComplete {
			workAction = domain.ActionAbandon
		}
		note := "feature " + string(action)
		if isAdoptedManual {
			note += " (adotado)"
		}
		if err := closeWork(repoDir, w, workAction, note); err != nil {
			return err
		}
	}
	return nil
}

// closeWork ends work w on the cerne's own initiative: done on
// ActionComplete, abandoned on ActionAbandon, with note on the entity and
// on the work.done/work.abandoned event, marked via=auto.
func closeWork(repoDir string, w store.Work, workAction domain.Action, note string) error {
	_, err := closeWorkRecord(repoDir, w, workAction, note, map[string]string{"note": note, "via": "auto"})
	return err
}

// closeWorkEntity is the explicit close (FinishWork and work start --from):
// the record of closeWorkRecord plus the end of the work's runbook run
// (F-0873), if it has one. The automatic closes do not end runs. It returns
// the new state.
func closeWorkEntity(repoDir string, w store.Work, workAction domain.Action, note string, payload map[string]string) (domain.WorkState, error) {
	newState, err := closeWorkRecord(repoDir, w, workAction, note, payload)
	if err != nil {
		return "", err
	}
	if r, err := store.LoadRun(repoDir, w.ID); err == nil {
		r.State = store.RunClosed
		if err := store.SaveRun(repoDir, r); err != nil {
			return "", err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	return newState, nil
}

// closeWorkRecord is the transition, the note and the work.done/work.abandoned
// event with payload; it returns the new state.
func closeWorkRecord(repoDir string, w store.Work, workAction domain.Action, note string, payload map[string]string) (domain.WorkState, error) {
	newState, err := domain.TransitionWork(w.State, workAction)
	if err != nil {
		return "", err
	}
	eventType := "work.done"
	if newState == domain.WorkAbandoned {
		eventType = "work.abandoned"
	}
	w.State = newState
	w.Note = note
	w.UpdatedAt = time.Now().UTC()
	if err := store.SaveWork(repoDir, w); err != nil {
		return "", err
	}
	if err := store.AppendEvent(repoDir, domain.NewEvent(w.ID, eventType, payload, EventActor())); err != nil {
		return "", err
	}
	return newState, nil
}

// planningWorks (F-0341) picks the planning works of featureID out of works:
// active, manual, refs exactly [featureID], and created before the feature's
// last feature.started — or at any time if it never entered development. The
// shape, never the title, makes a planning work; `feature start`, the exit
// from the live cycle and `work start` all ask this one function. state is
// the feature's state before the transition at hand: past approved with no
// feature.started in the log (legacy or imported), "never entered
// development" cannot be told, so there are none — the work-outlived-feature
// lint is the net.
func planningWorks(repoDir string, works []store.Work, featureID string, state domain.FeatureState) ([]store.Work, error) {
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		return nil, err
	}
	var lastStart time.Time
	for _, ev := range events {
		if ev.EntityID == featureID && ev.Type == eventTypes[domain.ActionStart] && ev.At.After(lastStart) {
			lastStart = ev.At
		}
	}
	if lastStart.IsZero() && state != domain.StateCandidate && state != domain.StateApproved {
		return nil, nil
	}
	var out []store.Work
	for _, w := range works {
		if w.State != domain.WorkActive || w.Auto || len(w.Refs) != 1 || w.Refs[0] != featureID {
			continue
		}
		if !lastStart.IsZero() && !w.CreatedAt.Before(lastStart) {
			continue // opened during development: not planning
		}
		out = append(out, w)
	}
	return out, nil
}

// closePlanningWorks (F-0341) closes every planning work of featureID (in
// state before the transition), of any actor, with workAction and note — the
// same transition and event as closeAutoWorks, so it lands in the caller's
// transition commit.
func closePlanningWorks(repoDir, featureID string, state domain.FeatureState, workAction domain.Action, note string) error {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return err
	}
	planning, err := planningWorks(repoDir, works, featureID, state)
	if err != nil {
		return err
	}
	for _, w := range planning {
		if err := closeWork(repoDir, w, workAction, note); err != nil {
			return err
		}
	}
	return nil
}
