package server

import (
	"net/http"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestTransitionRevert (F-0169 crit. 6): POST .../transition {"action":"revert"}
// on an approved feature walks it back to candidate through the generic
// transition endpoint — the UI is human interaction by definition, so no actor
// gate runs at this layer (same stance as approve/accept).
func TestTransitionRevert(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateApproved)
	commitBackend(t, repoDir)
	t.Setenv("CERNE_ACTOR", "agent") // proves the server path carries no gate

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition",
		`{"action":"revert","reason":"aprovada por engano"}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got["id"] != "F-0001" || got["from"] != "approved" || got["to"] != "candidate" {
		t.Fatalf("body = %v, want id/from/to of the revert transition", got)
	}

	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateCandidate {
		t.Fatalf("state = %s, want candidate", f.State)
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := events[len(events)-1]
	if last.Type != "feature.reverted" || last.Payload["reason"] != "aprovada por engano" {
		t.Fatalf("last event = %+v, want feature.reverted with reason payload", last)
	}
}

// TestTransitionRevertWrongState: revert on a candidate is refused with 422,
// same as any other invalid domain transition.
func TestTransitionRevertWrongState(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition",
		`{"action":"revert","reason":"nada a reverter"}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateCandidate {
		t.Fatalf("state after refused revert = %s, want candidate (untouched)", f.State)
	}
}
