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
	if _, err := CreateFeature(repoDir, projDir, "Corrige login", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}

	r, err := CreateExternalReference(repoDir, projDir,
		"  Artigo sobre PKCE  ", "https://example.com/pkce",
		"# PKCE\n\nO fluxo PKCE protege o code grant.", []string{"F-0001"}, nil, nil, false)
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

	if _, err := CreateExternalReference(repoDir, projDir, "T", "", "c", nil, nil, nil, false); err == nil {
		t.Fatalf("empty url should error")
	}
	if _, err := CreateExternalReference(repoDir, projDir, "T", "http://x", "c", []string{"F-9999"}, nil, nil, false); err == nil {
		t.Fatalf("dangling feature informs target should error")
	}
	// Dangling runbook/note targets error too (F-0108: RB-/N- accepted only when they exist).
	if _, err := CreateExternalReference(repoDir, projDir, "T", "http://x", "c", []string{"RB-9999"}, nil, nil, false); err == nil {
		t.Fatalf("dangling runbook informs target should error")
	}
	if _, err := CreateExternalReference(repoDir, projDir, "T", "http://x", "c", []string{"N-9999"}, nil, nil, false); err == nil {
		t.Fatalf("dangling note informs target should error")
	}
	// A reference id (R-) is not a valid informs target; the error names the accepted kinds.
	if _, err := CreateExternalReference(repoDir, projDir, "T", "http://x", "c", []string{"R-1"}, nil, nil, false); err == nil {
		t.Fatalf("reference informs target should error")
	}
	if _, err := CreateExternalReference(repoDir, projDir, "T", "http://x", "c", []string{"X-1"}, nil, nil, false); err == nil {
		t.Fatalf("unknown-kind informs target should error")
	}
	// Nothing was written.
	if refs, _ := store.LoadAllReferences(repoDir); len(refs) != 0 {
		t.Fatalf("failed creates persisted %d ref(s), want 0", len(refs))
	}
}

// TestCreateExternalReferenceInformsRunbookNote: F-0108 — an external source may
// inform a runbook (RB-) or a note (N-), not only features/decisions. The backlink
// is stored on the reference and the generic reverse lookup resolves it.
func TestCreateExternalReferenceInformsRunbookNote(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateRunbook(repoDir, projDir, "Deploy do serviço", "# Deploy\n\npassos"); err != nil {
		t.Fatalf("CreateRunbook: %v", err)
	}
	if _, err := CreateNote(repoDir, projDir, "Gotcha do build", "corpo", nil, nil, nil, false, false); err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	rbRef, err := CreateExternalReference(repoDir, projDir, "Doc do fornecedor", "http://vendor/x", "conteúdo", []string{"RB-0001"}, nil, nil, false)
	if err != nil {
		t.Fatalf("CreateExternalReference --informs RB-0001: %v", err)
	}
	if len(rbRef.Informs) != 1 || rbRef.Informs[0] != "RB-0001" {
		t.Fatalf("rbRef.Informs = %v, want [RB-0001]", rbRef.Informs)
	}
	nRef, err := CreateExternalReference(repoDir, projDir, "Artigo da gotcha", "http://blog/y", "conteúdo", []string{"N-0001"}, nil, nil, false)
	if err != nil {
		t.Fatalf("CreateExternalReference --informs N-0001: %v", err)
	}
	if len(nRef.Informs) != 1 || nRef.Informs[0] != "N-0001" {
		t.Fatalf("nRef.Informs = %v, want [N-0001]", nRef.Informs)
	}

	// The generic reverse lookup resolves the backlink for a runbook target.
	informing, err := ReferencesInforming(repoDir, "RB-0001")
	if err != nil {
		t.Fatalf("ReferencesInforming(RB-0001): %v", err)
	}
	if len(informing) != 1 || informing[0].ID != rbRef.ID {
		t.Fatalf("ReferencesInforming(RB-0001) = %+v, want [%s]", informing, rbRef.ID)
	}
}

// TestCreateNoteInformsRunbook: F-0108 — validateInforms is shared, so a note may
// also inform a runbook.
func TestCreateNoteInformsRunbook(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateRunbook(repoDir, projDir, "Recuperação", "# Recovery\n\npassos"); err != nil {
		t.Fatalf("CreateRunbook: %v", err)
	}
	n, err := CreateNote(repoDir, projDir, "Nota operacional", "corpo", []string{"RB-0001"}, nil, nil, false, false)
	if err != nil {
		t.Fatalf("CreateNote --informs RB-0001: %v", err)
	}
	if len(n.Informs) != 1 || n.Informs[0] != "RB-0001" {
		t.Fatalf("note.Informs = %v, want [RB-0001]", n.Informs)
	}
}

// TestReferencesInforming: the reverse lookup returns the external refs whose
// Informs backlink contains the entity id.
func TestReferencesInforming(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Feat", "", "", nil, nil); err != nil {
		t.Fatalf("CreateFeature: %v", err)
	}
	if _, err := CreateExternalReference(repoDir, projDir, "A", "http://a", "ca", []string{"F-0001"}, nil, nil, false); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := CreateExternalReference(repoDir, projDir, "B", "http://b", "cb", nil, nil, nil, false); err != nil {
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
