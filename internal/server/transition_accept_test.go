package server

import (
	"net/http"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestTransitionAccept (F-0131 crit. 3): POST .../transition {"action":"accept"}
// on an implemented feature moves it to done — the UI is human interaction by
// definition, so no gate check runs at this layer.
func TestTransitionAccept(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateImplemented)
	commitBackend(t, repoDir)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition",
		`{"action":"accept"}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got["id"] != "F-0001" || got["from"] != "implemented" || got["to"] != "done" {
		t.Fatalf("body = %v, want id/from/to of the accept transition", got)
	}

	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateDone {
		t.Fatalf("state = %s, want done", f.State)
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) == 0 || events[len(events)-1].Type != "feature.accepted" {
		t.Fatalf("events = %+v, want trailing feature.accepted", events)
	}
}

// TestTransitionAcceptWrongState: accept on a candidate is refused with 422,
// same as any other invalid domain transition.
func TestTransitionAcceptWrongState(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition",
		`{"action":"accept"}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateCandidate {
		t.Fatalf("state after failed gate = %s, want candidate", f.State)
	}
}
