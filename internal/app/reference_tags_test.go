package app

import (
	"errors"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestCreateExternalReferenceWithTags: facets/components are validated and
// normalized (lower/slug/dedup) and persisted on the reference, mirroring notes.
func TestCreateExternalReferenceWithTags(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	r, err := CreateExternalReference(repoDir, projDir,
		"Artigo SSH no Gitea", "https://example.com/ssh",
		"# SSH\n\nConfiguração de known_hosts.", nil,
		[]string{"Infra", "INFRA", "code"}, []string{"Gitea", "KPack"})
	if err != nil {
		t.Fatalf("CreateExternalReference: %v", err)
	}
	if len(r.Facets) != 2 || r.Facets[0] != "infra" || r.Facets[1] != "code" {
		t.Fatalf("facets = %v, want [infra code]", r.Facets)
	}
	if len(r.Components) != 2 || r.Components[0] != "gitea" || r.Components[1] != "kpack" {
		t.Fatalf("components = %v, want [gitea kpack]", r.Components)
	}
	got, err := store.LoadReference(repoDir, r.ID)
	if err != nil {
		t.Fatalf("LoadReference: %v", err)
	}
	if len(got.Facets) != 2 || len(got.Components) != 2 {
		t.Fatalf("persisted tags = %v / %v", got.Facets, got.Components)
	}
}

// TestCreateExternalReferenceInvalidFacet: an unknown facet aborts with
// ErrUnknownFacet and writes nothing (validated before any side effect).
func TestCreateExternalReferenceInvalidFacet(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	_, err := CreateExternalReference(repoDir, projDir, "T", "http://x", "c", nil,
		[]string{"prod"}, nil)
	if !errors.Is(err, domain.ErrUnknownFacet) {
		t.Fatalf("err = %v, want errors.Is ErrUnknownFacet", err)
	}
	if refs, _ := store.LoadAllReferences(repoDir); len(refs) != 0 {
		t.Fatalf("invalid create persisted %d ref(s), want 0", len(refs))
	}
}
