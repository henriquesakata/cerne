package app

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// assertClosedByFrom: the origin of `work start --from` is closed by the
// explicit handoff (note, done), never marked as an automatic close.
func assertClosedByFrom(t *testing.T, repoDir, id, note string) {
	t.Helper()
	w, err := store.LoadWork(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != domain.WorkDone || w.Note != note {
		t.Fatalf("%s = %s %q, want done %q", id, w.State, w.Note, note)
	}
	evs, _ := store.ReadEvents(repoDir)
	for _, ev := range evs {
		if ev.EntityID == id && ev.Type == "work.done" {
			if ev.Payload["via"] == "auto" || ev.Payload["note"] != note {
				t.Fatalf("work.done payload = %v, want note %q and no via=auto", ev.Payload, note)
			}
			return
		}
	}
	t.Fatalf("no work.done event for %s", id)
}

// Round 3, A: the automatic closes (closeAutoWorks, F-0341, adoption) go
// through closeWork and never end a runbook run; only a manual close does.
func TestAutomaticCloseLeavesRunOpen(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	a := startW(t, repoDir, projDir, "Execução de runbook", nil)
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		return "seed run", store.SaveRun(repoDir, store.Run{ID: a.ID, Runbook: "RB-0001", State: store.RunOpen})
	})
	if err != nil {
		t.Fatal(err)
	}
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		w, _ := store.LoadWork(repoDir, a.ID)
		return "auto close", closeWork(repoDir, w, domain.ActionComplete, "feature complete")
	})
	if err != nil {
		t.Fatal(err)
	}
	if r, err := store.LoadRun(repoDir, a.ID); err != nil || r.State != store.RunOpen {
		t.Fatalf("run after an automatic close = %+v, %v; want it still open", r, err)
	}
}
