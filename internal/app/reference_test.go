package app

import (
	"os"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestCreateExternalReference: registers an external source — entity carries
// URL/External/Informs, the pasted content lands in the content doc, a
// reference.created event is appended, and the backend is committed.
func TestCreateExternalReference(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	// A feature to link the source to (the fix it motivated).
	if _, err := CreateFeature(repoDir, projDir, "Corrige login", ""); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}

	r, err := CreateExternalReference(repoDir, projDir,
		"  Artigo sobre PKCE  ", "https://example.com/pkce",
		"# PKCE\n\nO fluxo PKCE protege o code grant.", []string{"F-0001"})
	if err != nil {
		t.Fatalf("CreateExternalReference: %v", err)
	}
	if r.ID != "R-0001" || r.Title != "Artigo sobre PKCE" || !r.External {
		t.Fatalf("ref = %+v, want R-0001 trimmed title External=true", r)
	}
	if r.URL != "https://example.com/pkce" || r.State != domain.ReferenceCurrent {
		t.Fatalf("ref url/state = %q/%q", r.URL, r.State)
	}
	if len(r.Informs) != 1 || r.Informs[0] != "F-0001" {
		t.Fatalf("ref.Informs = %v, want [F-0001]", r.Informs)
	}

	got, err := store.LoadReference(repoDir, "R-0001")
	if err != nil || !got.External || got.URL != "https://example.com/pkce" {
		t.Fatalf("LoadReference = %+v, %v; want persisted external ref", got, err)
	}

	doc, err := os.ReadFile(ReferenceDocPath(repoDir, "R-0001"))
	if err != nil {
		t.Fatalf("read content doc: %v", err)
	}
	for _, want := range []string{"cerne_id: R-0001", "O fluxo PKCE protege"} {
		if !strings.Contains(string(doc), want) {
			t.Fatalf("content doc missing %q:\n%s", want, doc)
		}
	}

	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	found := false
	for _, e := range evs {
		if e.EntityID == "R-0001" && e.Type == "reference.created" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing reference.created event for R-0001: %+v", evs)
	}
}

// TestCreateExternalReferenceValidation: empty url and a dangling informs target
// both error and write nothing.
func TestCreateExternalReferenceValidation(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)

	if _, err := CreateExternalReference(repoDir, projDir, "T", "", "c", nil); err == nil {
		t.Fatalf("empty url should error")
	}
	if _, err := CreateExternalReference(repoDir, projDir, "T", "http://x", "c", []string{"F-9999"}); err == nil {
		t.Fatalf("dangling informs target should error")
	}
	if _, err := CreateExternalReference(repoDir, projDir, "T", "http://x", "c", []string{"X-1"}); err == nil {
		t.Fatalf("non-F/D informs target should error")
	}
	// Nothing was written.
	if refs, _ := store.LoadAllReferences(repoDir); len(refs) != 0 {
		t.Fatalf("failed creates persisted %d ref(s), want 0", len(refs))
	}
}

// TestReferencesInforming: the reverse lookup returns the external refs whose
// Informs backlink contains the entity id.
func TestReferencesInforming(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Feat", ""); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if _, err := CreateExternalReference(repoDir, projDir, "A", "http://a", "ca", []string{"F-0001"}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := CreateExternalReference(repoDir, projDir, "B", "http://b", "cb", nil); err != nil {
		t.Fatalf("create B: %v", err)
	}

	got, err := ReferencesInforming(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("ReferencesInforming: %v", err)
	}
	if len(got) != 1 || got[0].ID != "R-0001" {
		t.Fatalf("ReferencesInforming(F-0001) = %+v, want [R-0001]", got)
	}
}
