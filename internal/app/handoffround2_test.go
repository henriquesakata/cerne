package app

import (
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// Round 2, E: a work with no actor still gets a handoff that reads back.
func TestHandoffWorkEmptyActor(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	w := startW(t, repoDir, projDir, "Operação longa", nil)
	w.Actor = ""
	if err := store.SaveWork(repoDir, w); err != nil {
		t.Fatal(err)
	}
	id, err := HandoffWork(repoDir, projDir, w.ID, "brief", "n", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h, _ := store.LoadHandoff(repoDir, id); h.Actor == "" {
		t.Fatalf("handoff = %+v, want a non-empty actor", h)
	}
}

// Round 2, A: a non-canonical id (w-0001) is the same work everywhere: the
// origin closes once, its handoff prints once, the file lands under W-0001.
func TestWorkIDsAreCanonical(t *testing.T) {
	t.Setenv("CERNE_SESSION", "sess-me")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	seedActiveWork(t, repoDir, "W-0001", defaultWorkActor(), "F-0001", false)
	if err := BindSession(filepath.Dir(repoDir), "sess-me", "W-0001"); err != nil {
		t.Fatal(err)
	}
	id, err := HandoffWork(repoDir, projDir, "w-0001", "brief", "n1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h, _ := store.LoadHandoff(repoDir, id); h.FromWork != "W-0001" {
		t.Fatalf("handoff must point at W-0001, got %+v", h)
	}
	if n := countEvents(t, repoDir, "W-0001", "work.handoff"); n != 1 {
		t.Fatalf("work.handoff events under W-0001 = %d, want 1", n)
	}
	w, err := StartWork(repoDir, projDir, "Retomada do plano", nil, "", false, nil, "w-0001")
	if err != nil {
		t.Fatal(err)
	}
	if w.From != "W-0001" || len(w.Handoffs) != 1 || len(w.Superseded) != 0 {
		t.Fatalf("from %q handoffs %+v superseded %v, want W-0001, one handoff, none superseded", w.From, w.Handoffs, w.Superseded)
	}
	if n := countEvents(t, repoDir, "W-0001", "work.done"); n != 1 {
		t.Fatalf("work.done events on W-0001 = %d, want 1", n)
	}
}
