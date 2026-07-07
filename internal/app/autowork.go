package app

import (
	"os"
	"slices"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
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
func autoWorkActor() string {
	if a := os.Getenv("CERNE_ACTOR"); a != "" {
		return a
	}
	return defaultWorkActor()
}

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

// closeAutoWorks ends every still-active AUTO work referencing the feature
// that just left in_development: done on complete, abandoned on supersede.
// Manual works (Auto=false) are never touched — closing those is the owner's
// call (the stale-work lint nags about forgotten ones).
func closeAutoWorks(repoDir, featureID string, action domain.Action) error {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return err
	}
	for _, w := range works {
		if w.State != domain.WorkActive || !w.Auto || !slices.Contains(w.Refs, featureID) {
			continue
		}
		workAction := domain.ActionComplete
		eventType := "work.done"
		if action != domain.ActionComplete {
			workAction = domain.ActionAbandon
			eventType = "work.abandoned"
		}
		newState, err := domain.TransitionWork(w.State, workAction)
		if err != nil {
			return err
		}
		w.State = newState
		w.Note = "feature " + string(action)
		w.UpdatedAt = time.Now().UTC()
		if err := store.SaveWork(repoDir, w); err != nil {
			return err
		}
		ev := domain.NewEvent(w.ID, eventType, map[string]string{"note": w.Note, "via": "auto"}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return err
		}
	}
	return nil
}
