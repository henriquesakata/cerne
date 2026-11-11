package app

import (
	"errors"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// mkTaggedDecision: an active decision with the given tags (helper for retag tests).
func mkTaggedDecision(t *testing.T, repoDir, projDir string, facets, components []string) store.Decision {
	t.Helper()
	d, err := CreateDecision(repoDir, projDir, "Decisão", "corpo", facets, components, true) // fixture helper: several same-titled decisions on purpose
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	return d
}

// TestRetagDecisionReplacesAxes: both axes set → replaced with normalized values.
func TestRetagDecisionReplacesAxes(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	d := mkTaggedDecision(t, repoDir, projDir, []string{"code"}, []string{"old"})
	got, err := RetagDecision(repoDir, projDir, d.ID, []string{"Auth"}, []string{"Keycloak"}, true, true)
	if err != nil {
		t.Fatalf("RetagDecision: %v", err)
	}
	if len(got.Facets) != 1 || got.Facets[0] != "auth" || len(got.Components) != 1 || got.Components[0] != "keycloak" {
		t.Fatalf("retag = %v / %v, want [auth] / [keycloak]", got.Facets, got.Components)
	}
	reload, _ := store.LoadDecision(repoDir, d.ID)
	if len(reload.Facets) != 1 || reload.Facets[0] != "auth" {
		t.Fatalf("persisted facets = %v", reload.Facets)
	}
}

// TestRetagDecisionPartialKeepsOtherAxis: setting only components preserves facets.
func TestRetagDecisionPartialKeepsOtherAxis(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	d := mkTaggedDecision(t, repoDir, projDir, []string{"auth"}, []string{"keycloak"})
	got, err := RetagDecision(repoDir, projDir, d.ID, nil, []string{"gitea"}, false, true)
	if err != nil {
		t.Fatalf("RetagDecision: %v", err)
	}
	if len(got.Facets) != 1 || got.Facets[0] != "auth" {
		t.Fatalf("facets changed = %v, want preserved [auth]", got.Facets)
	}
	if len(got.Components) != 1 || got.Components[0] != "gitea" {
		t.Fatalf("components = %v, want [gitea]", got.Components)
	}
}

// TestRetagDecisionClearAxis: set=true with an empty slice clears that axis.
func TestRetagDecisionClearAxis(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	d := mkTaggedDecision(t, repoDir, projDir, []string{"auth"}, []string{"keycloak"})
	got, err := RetagDecision(repoDir, projDir, d.ID, nil, nil, true, false)
	if err != nil {
		t.Fatalf("RetagDecision: %v", err)
	}
	if len(got.Facets) != 0 {
		t.Fatalf("facets = %v, want cleared", got.Facets)
	}
	if len(got.Components) != 1 {
		t.Fatalf("components changed = %v, want preserved", got.Components)
	}
}

// TestRetagDecisionInvalidFacetNoOp: an unknown facet errors and leaves the decision as-is.
func TestRetagDecisionInvalidFacetNoOp(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	d := mkTaggedDecision(t, repoDir, projDir, []string{"auth"}, []string{"keycloak"})
	_, err := RetagDecision(repoDir, projDir, d.ID, []string{"prod"}, nil, true, false)
	if !errors.Is(err, domain.ErrUnknownFacet) {
		t.Fatalf("err = %v, want ErrUnknownFacet", err)
	}
	reload, _ := store.LoadDecision(repoDir, d.ID)
	if len(reload.Facets) != 1 || reload.Facets[0] != "auth" {
		t.Fatalf("decision mutated on invalid retag: facets = %v", reload.Facets)
	}
}

// TestRetagDecisionNotFoundOrSuperseded: missing id and superseded decision both error.
func TestRetagDecisionNotFoundOrSuperseded(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := RetagDecision(repoDir, projDir, "D-9999", []string{"auth"}, nil, true, false); err == nil {
		t.Fatalf("retag of missing decision should error")
	}
	d := mkTaggedDecision(t, repoDir, projDir, nil, nil)
	d2 := mkTaggedDecision(t, repoDir, projDir, nil, nil)
	if _, err := TransitionDecision(repoDir, projDir, d.ID, d2.ID, "obsoleta"); err != nil {
		t.Fatalf("TransitionDecision: %v", err)
	}
	if _, err := RetagDecision(repoDir, projDir, d.ID, []string{"auth"}, nil, true, false); err == nil {
		t.Fatalf("retag of superseded decision should error")
	}
}
