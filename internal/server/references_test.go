package server

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestCreateReferenceEndpoint: POST /api/references registers an external
// source, persisting URL/External/Informs (linked to the seeded F-0001).
func TestCreateReferenceEndpoint(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t) // seeds F-0001
	var got struct {
		Reference store.Reference `json:"reference"`
	}
	body := `{"title":"Artigo PKCE","url":"https://example.com/pkce","content":"# PKCE\n\ntexto","informs":["F-0001"]}`
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/references", body, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	r := got.Reference
	if r.ID != "R-0001" || !r.External || r.URL != "https://example.com/pkce" {
		t.Fatalf("reference = %+v, want R-0001 external with url", r)
	}
	if len(r.Informs) != 1 || r.Informs[0] != "F-0001" {
		t.Fatalf("informs = %v, want [F-0001]", r.Informs)
	}
}

// TestCreateReferenceWithTags: POST /api/references accepts facets/components and
// persists them (crit. 1); an out-of-set facet is a 422 with nothing written.
func TestCreateReferenceWithTags(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	var got struct {
		Reference store.Reference `json:"reference"`
	}
	body := `{"title":"Infra doc","url":"https://example.com/gitea","content":"x","facets":["infra"],"components":["gitea"]}`
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/references", body, &got)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Reference.Facets) != 1 || got.Reference.Facets[0] != "infra" {
		t.Fatalf("facets = %v, want [infra]", got.Reference.Facets)
	}
	if len(got.Reference.Components) != 1 || got.Reference.Components[0] != "gitea" {
		t.Fatalf("components = %v, want [gitea]", got.Reference.Components)
	}
	reload, err := store.LoadReference(repoDir, "R-0001")
	if err != nil {
		t.Fatalf("LoadReference: %v", err)
	}
	if len(reload.Facets) != 1 || reload.Facets[0] != "infra" || len(reload.Components) != 1 || reload.Components[0] != "gitea" {
		t.Fatalf("persisted tags = %v / %v, want [infra] / [gitea]", reload.Facets, reload.Components)
	}
}

// TestCreateReferenceUnknownFacet: a facet outside the closed set is a 422 and
// nothing is persisted (crit. 1).
func TestCreateReferenceUnknownFacet(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	var got map[string]string
	body := `{"title":"X","url":"http://x","content":"c","facets":["prod"]}`
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/references", body, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if _, err := store.LoadReference(repoDir, "R-0001"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("LoadReference err = %v, want ErrNotFound (nothing written)", err)
	}
}

// TestReferenceDetailHasTags: GET /api/references/{id} includes facets/components
// (already serialized via the embedded store.Reference) — guard for crit. 2.
func TestReferenceDetailHasTags(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	if _, err := app.CreateExternalReference(repoDir, projDir, "Doc", "http://a", "c", nil, []string{"infra"}, []string{"gitea"}, false); err != nil {
		t.Fatalf("CreateExternalReference: %v", err)
	}
	var got struct {
		Reference store.Reference `json:"reference"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/references/R-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Reference.Facets) != 1 || got.Reference.Facets[0] != "infra" {
		t.Fatalf("facets = %v, want [infra]", got.Reference.Facets)
	}
	if len(got.Reference.Components) != 1 || got.Reference.Components[0] != "gitea" {
		t.Fatalf("components = %v, want [gitea]", got.Reference.Components)
	}
}

// TestCreateReferenceEndpointDangling: a non-existent informs target is a 422.
func TestCreateReferenceEndpointDangling(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	var got map[string]string
	body := `{"title":"X","url":"http://x","content":"c","informs":["F-9999"]}`
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/references", body, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
}

// TestFeatureDetailInformedBy: a feature's detail lists the external refs that
// inform it (the reverse "Fontes" backlink).
func TestFeatureDetailInformedBy(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	if _, err := app.CreateExternalReference(repoDir, projDir, "Fonte", "http://a", "c", []string{"F-0001"}, nil, nil, false); err != nil {
		t.Fatalf("CreateExternalReference: %v", err)
	}
	var got struct {
		Feature    store.Feature    `json:"feature"`
		InformedBy []store.Reference `json:"informed_by"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.InformedBy) != 1 || got.InformedBy[0].ID != "R-0001" || !got.InformedBy[0].External {
		t.Fatalf("informed_by = %+v, want [R-0001 external]", got.InformedBy)
	}
}

// TestDecisionDetailInformedBy: same reverse backlink on a decision.
func TestDecisionDetailInformedBy(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	now := time.Now().UTC()
	if err := store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "Dec", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	commitBackend(t, repoDir)
	if _, err := app.CreateExternalReference(repoDir, projDir, "Fonte", "http://a", "c", []string{"D-0001"}, nil, nil, false); err != nil {
		t.Fatalf("CreateExternalReference: %v", err)
	}
	var got struct {
		Decision   store.Decision   `json:"decision"`
		InformedBy []store.Reference `json:"informed_by"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/decisions/D-0001", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.InformedBy) != 1 || got.InformedBy[0].ID != "R-0001" {
		t.Fatalf("informed_by = %+v, want [R-0001]", got.InformedBy)
	}
}
