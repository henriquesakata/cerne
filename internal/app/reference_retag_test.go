package app

import (
	"errors"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// mkTaggedReference: a current external reference with the given tags (helper for retag tests).
func mkTaggedReference(t *testing.T, repoDir, projDir string, facets, components []string) store.Reference {
	t.Helper()
	r, err := CreateExternalReference(repoDir, projDir,
		"Artigo", "https://example.com/x", "# corpo", nil, facets, components)
	if err != nil {
		t.Fatalf("CreateExternalReference: %v", err)
	}
	return r
}

// TestRetagReferenceReplacesAxes: both axes set → replaced with normalized values.
func TestRetagReferenceReplacesAxes(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	r := mkTaggedReference(t, repoDir, projDir, []string{"code"}, []string{"old"})
	got, err := RetagReference(repoDir, projDir, r.ID, []string{"Auth"}, []string{"Keycloak"}, true, true)
	if err != nil {
		t.Fatalf("RetagReference: %v", err)
	}
	if len(got.Facets) != 1 || got.Facets[0] != "auth" || len(got.Components) != 1 || got.Components[0] != "keycloak" {
		t.Fatalf("retag = %v / %v, want [auth] / [keycloak]", got.Facets, got.Components)
	}
	reload, _ := store.LoadReference(repoDir, r.ID)
	if len(reload.Facets) != 1 || reload.Facets[0] != "auth" {
		t.Fatalf("persisted facets = %v", reload.Facets)
	}
}

// TestRetagReferencePartialKeepsOtherAxis: setting only components preserves facets.
func TestRetagReferencePartialKeepsOtherAxis(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	r := mkTaggedReference(t, repoDir, projDir, []string{"auth"}, []string{"keycloak"})
	got, err := RetagReference(repoDir, projDir, r.ID, nil, []string{"gitea"}, false, true)
	if err != nil {
		t.Fatalf("RetagReference: %v", err)
	}
	if len(got.Facets) != 1 || got.Facets[0] != "auth" {
		t.Fatalf("facets changed = %v, want preserved [auth]", got.Facets)
	}
	if len(got.Components) != 1 || got.Components[0] != "gitea" {
		t.Fatalf("components = %v, want [gitea]", got.Components)
	}
}

// TestRetagReferenceClearAxis: set=true with an empty slice clears that axis.
func TestRetagReferenceClearAxis(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	r := mkTaggedReference(t, repoDir, projDir, []string{"auth"}, []string{"keycloak"})
	got, err := RetagReference(repoDir, projDir, r.ID, nil, nil, true, false)
	if err != nil {
		t.Fatalf("RetagReference: %v", err)
	}
	if len(got.Facets) != 0 {
		t.Fatalf("facets = %v, want cleared", got.Facets)
	}
	if len(got.Components) != 1 {
		t.Fatalf("components changed = %v, want preserved", got.Components)
	}
}

// TestRetagReferenceInvalidFacetNoOp: an unknown facet errors and leaves the reference as-is.
func TestRetagReferenceInvalidFacetNoOp(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	r := mkTaggedReference(t, repoDir, projDir, []string{"auth"}, []string{"keycloak"})
	_, err := RetagReference(repoDir, projDir, r.ID, []string{"prod"}, nil, true, false)
	if !errors.Is(err, domain.ErrUnknownFacet) {
		t.Fatalf("err = %v, want ErrUnknownFacet", err)
	}
	reload, _ := store.LoadReference(repoDir, r.ID)
	if len(reload.Facets) != 1 || reload.Facets[0] != "auth" {
		t.Fatalf("reference mutated on invalid retag: facets = %v", reload.Facets)
	}
}

// TestRetagReferenceNotFoundOrObsolete: missing id and obsolete reference both error.
func TestRetagReferenceNotFoundOrObsolete(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := RetagReference(repoDir, projDir, "R-9999", []string{"auth"}, nil, true, false); err == nil {
		t.Fatalf("retag of missing reference should error")
	}
	r := mkTaggedReference(t, repoDir, projDir, []string{"auth"}, nil)
	// Flip it to obsolete directly (no app transition exists for references yet).
	r.State = domain.ReferenceObsolete
	if err := store.SaveReference(repoDir, r); err != nil {
		t.Fatalf("SaveReference obsolete: %v", err)
	}
	commitBackend(t, repoDir)
	if _, err := RetagReference(repoDir, projDir, r.ID, []string{"code"}, nil, true, false); err == nil {
		t.Fatalf("retag of obsolete reference should error")
	}
	reload, _ := store.LoadReference(repoDir, r.ID)
	if len(reload.Facets) != 1 || reload.Facets[0] != "auth" {
		t.Fatalf("obsolete reference mutated on refused retag: %v", reload.Facets)
	}
}

// TestRetagReferencePreservesURLInforms: retag touches only tags, not URL/External/Informs/body.
func TestRetagReferencePreservesURLInforms(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	d, err := CreateDecision(repoDir, projDir, "Decisão", "corpo", nil, nil)
	if err != nil {
		t.Fatalf("CreateDecision: %v", err)
	}
	r, err := CreateExternalReference(repoDir, projDir,
		"Artigo", "https://example.com/ssh", "# corpo", []string{d.ID}, []string{"code"}, nil)
	if err != nil {
		t.Fatalf("CreateExternalReference: %v", err)
	}
	got, err := RetagReference(repoDir, projDir, r.ID, []string{"infra"}, []string{"gitea"}, true, true)
	if err != nil {
		t.Fatalf("RetagReference: %v", err)
	}
	if got.URL != "https://example.com/ssh" || !got.External {
		t.Fatalf("retag changed URL/External = %q / %v", got.URL, got.External)
	}
	if len(got.Informs) != 1 || got.Informs[0] != d.ID {
		t.Fatalf("retag changed Informs = %v", got.Informs)
	}
	if !got.UpdatedAt.After(r.UpdatedAt) {
		t.Fatalf("UpdatedAt did not advance: %v !> %v", got.UpdatedAt, r.UpdatedAt)
	}
}
