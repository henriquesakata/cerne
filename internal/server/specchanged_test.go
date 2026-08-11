package server

import (
	"net/http"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestFeatureDetailSpecChangedAfterApproval pins F-0262: the feature page needs
// to warn that the approval is stale, so the detail payload carries the fact
// derived from the event log — same derivation the lint rule uses.
func TestFeatureDetailSpecChangedAfterApproval(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Editada após aprovar", domain.StateApproved)
	seedFeature(t, repoDir, "F-0002", "Aprovada intacta", domain.StateApproved)

	base := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	events := []domain.Event{
		{ID: "e1", At: base, EntityID: "F-0001", Type: "feature.approved", Actor: "u"},
		{ID: "e2", At: base.Add(time.Minute), EntityID: "F-0001", Type: "feature.doc_edited",
			Payload: map[string]string{"doc": "spec", "state": "approved"}, Actor: "u"},
		{ID: "e3", At: base, EntityID: "F-0002", Type: "feature.approved", Actor: "u"},
	}
	for _, e := range events {
		if err := store.AppendEvent(repoDir, e); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	var got struct {
		SpecChanged bool `json:"spec_changed_after_approval"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got)
	if rec.Code != http.StatusOK || !got.SpecChanged {
		t.Fatalf("F-0001: status %d, spec_changed_after_approval = %v, want true", rec.Code, got.SpecChanged)
	}
	rec = doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0002", "", &got)
	if rec.Code != http.StatusOK || got.SpecChanged {
		t.Fatalf("F-0002: status %d, spec_changed_after_approval = %v, want false", rec.Code, got.SpecChanged)
	}
}
