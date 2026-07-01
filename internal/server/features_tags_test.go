package server

import (
	"net/http"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestFeatureTagsExposed pins the contract the kanban/feature-page UI depends
// on: a feature's facets/components (added to store.Feature for F-0100, mirroring
// the F-0099 backend) are serialized in both GET /api/state and
// GET /api/features/{id}. No server code change is needed — the struct is
// embedded in the responses — so this test guards the contract against a future
// regression.
func TestFeatureTagsExposed(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	now := time.Now().UTC()
	f := store.Feature{
		ID: "F-0001", Title: "Feature A", State: domain.StateCandidate,
		Facets: []string{"infra"}, Components: []string{"gitback"},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	srv := New(repoDir, projDir, "")

	// GET /api/state — the feature in the roster carries its tags.
	var state struct {
		Features []store.Feature `json:"features"`
	}
	if rec := doJSON(t, srv, http.MethodGet, "/api/state", "", &state); rec.Code != http.StatusOK {
		t.Fatalf("state status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var found *store.Feature
	for i := range state.Features {
		if state.Features[i].ID == "F-0001" {
			found = &state.Features[i]
		}
	}
	if found == nil {
		t.Fatalf("F-0001 absent from /api/state features: %+v", state.Features)
	}
	if len(found.Facets) != 1 || found.Facets[0] != "infra" || len(found.Components) != 1 || found.Components[0] != "gitback" {
		t.Fatalf("state tags = %v / %v, want [infra] / [gitback]", found.Facets, found.Components)
	}

	// GET /api/features/{id} — the detail payload carries them too.
	var detail struct {
		Feature store.Feature `json:"feature"`
	}
	if rec := doJSON(t, srv, http.MethodGet, "/api/features/F-0001", "", &detail); rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(detail.Feature.Facets) != 1 || detail.Feature.Facets[0] != "infra" ||
		len(detail.Feature.Components) != 1 || detail.Feature.Components[0] != "gitback" {
		t.Fatalf("detail tags = %v / %v, want [infra] / [gitback]", detail.Feature.Facets, detail.Feature.Components)
	}
}
