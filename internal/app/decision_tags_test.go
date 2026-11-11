package app

import (
	"errors"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestCreateDecisionWithTags: facets/components are validated and normalized
// (lower/slug/dedup) and persisted on the decision, like a note.
func TestCreateDecisionWithTags(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	d, err := CreateDecision(repoDir, projDir, "Usar Keycloak para OIDC", "corpo real do ADR",
		[]string{"Auth", "AUTH", "infra"}, []string{"Keycloak", "Gitea"}, false)
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	if len(d.Facets) != 2 || d.Facets[0] != "auth" || d.Facets[1] != "infra" {
		t.Fatalf("facets = %v, want [auth infra]", d.Facets)
	}
	if len(d.Components) != 2 || d.Components[0] != "keycloak" || d.Components[1] != "gitea" {
		t.Fatalf("components = %v, want [keycloak gitea]", d.Components)
	}
	got, err := store.LoadDecision(repoDir, d.ID)
	if err != nil {
		t.Fatalf("LoadDecision: %v", err)
	}
	if len(got.Facets) != 2 || len(got.Components) != 2 {
		t.Fatalf("persisted tags = %v / %v", got.Facets, got.Components)
	}
}

// TestCreateDecisionInvalidFacet: an unknown facet aborts with ErrUnknownFacet
// and mints nothing (validated before NextID / any write).
func TestCreateDecisionInvalidFacet(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	_, err := CreateDecision(repoDir, projDir, "T", "body", []string{"prod"}, nil, false)
	if !errors.Is(err, domain.ErrUnknownFacet) {
		t.Fatalf("err = %v, want errors.Is ErrUnknownFacet", err)
	}
	if ds, _ := store.LoadAllDecisions(repoDir); len(ds) != 0 {
		t.Fatalf("invalid create persisted %d decision(s), want 0", len(ds))
	}
}
