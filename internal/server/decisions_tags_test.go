package server

import (
	"net/http"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestDecisionTagsExposed pins the contract the Decisions UI depends on: a
// decision's facets/components (added to store.Decision in F-0087) are
// serialized in both GET /api/state and GET /api/decisions/{id}. No server
// code change is needed — the struct is embedded — so this test guards the
// contract against a future regression.
func TestDecisionTagsExposed(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	now := time.Now().UTC()
	d := store.Decision{
		ID: "D-0001", Title: "Escolha de IdP", State: domain.DecisionActive,
		Facets: []string{"auth"}, Components: []string{"keycloak"},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.SaveDecision(repoDir, d); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	srv := New(repoDir, projDir, "")

	// GET /api/state — the decision in the roster carries its tags.
	var state struct {
		Decisions []store.Decision `json:"decisions"`
	}
	if rec := doJSON(t, srv, http.MethodGet, "/api/state", "", &state); rec.Code != http.StatusOK {
		t.Fatalf("state status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	var found *store.Decision
	for i := range state.Decisions {
		if state.Decisions[i].ID == "D-0001" {
			found = &state.Decisions[i]
		}
	}
	if found == nil {
		t.Fatalf("D-0001 absent from /api/state decisions: %+v", state.Decisions)
	}
	if len(found.Facets) != 1 || found.Facets[0] != "auth" || len(found.Components) != 1 || found.Components[0] != "keycloak" {
		t.Fatalf("state tags = %v / %v, want [auth] / [keycloak]", found.Facets, found.Components)
	}

	// GET /api/decisions/{id} — the detail payload carries them too.
	var detail struct {
		Decision store.Decision `json:"decision"`
	}
	if rec := doJSON(t, srv, http.MethodGet, "/api/decisions/D-0001", "", &detail); rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(detail.Decision.Facets) != 1 || detail.Decision.Facets[0] != "auth" ||
		len(detail.Decision.Components) != 1 || detail.Decision.Components[0] != "keycloak" {
		t.Fatalf("detail tags = %v / %v, want [auth] / [keycloak]", detail.Decision.Facets, detail.Decision.Components)
	}
}
