package cli

import (
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/rockt/cerne/internal/store"
)

// mkReference registers a current reference R-0001 with the given facet (empty
// to skip), reading content from a --file so execCerne never blocks on stdin.
func mkReference(t *testing.T, projDir, facet string) {
	t.Helper()
	contentFile := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(contentFile, []byte("# corpo"), 0o644); err != nil {
		t.Fatalf("write content: %v", err)
	}
	args := []string{"reference", "new", "Artigo", "--url", "https://example.com/x", "--file", contentFile, "--dir", projDir}
	if facet != "" {
		args = append(args, "--facet", facet)
	}
	if _, err := execCerne(t, args...); err != nil {
		t.Fatalf("reference new: %v", err)
	}
}

// TestReferenceRetagSetsTags: retag sets both axes on an existing reference.
func TestReferenceRetagSetsTags(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	mkReference(t, projDir, "")
	out, err := execCerne(t, "reference", "retag", "R-0001", "--facet", "infra", "--component", "Gitea", "--dir", projDir)
	if err != nil {
		t.Fatalf("reference retag: %v\n%s", err, out)
	}
	r, _ := store.LoadReference(repoDir, "R-0001")
	if len(r.Facets) != 1 || r.Facets[0] != "infra" || len(r.Components) != 1 || r.Components[0] != "gitea" {
		t.Fatalf("retag persisted = %v / %v", r.Facets, r.Components)
	}
}

// TestReferenceRetagPartialKeepsFacet: retagging only --component leaves facets
// intact (flag.Changed semantics).
func TestReferenceRetagPartialKeepsFacet(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	mkReference(t, projDir, "infra")
	if _, err := execCerne(t, "reference", "retag", "R-0001", "--component", "gitea", "--dir", projDir); err != nil {
		t.Fatalf("reference retag: %v", err)
	}
	r, _ := store.LoadReference(repoDir, "R-0001")
	if len(r.Facets) != 1 || r.Facets[0] != "infra" {
		t.Fatalf("facet not preserved: %v", r.Facets)
	}
	if len(r.Components) != 1 || r.Components[0] != "gitea" {
		t.Fatalf("component = %v", r.Components)
	}
}

// TestReferenceRetagInvalidFacet: an unknown facet fails the command and leaves
// the reference unchanged.
func TestReferenceRetagInvalidFacet(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	mkReference(t, projDir, "")
	if _, err := execCerne(t, "reference", "retag", "R-0001", "--facet", "prod", "--dir", projDir); err == nil {
		t.Fatalf("retag --facet prod should fail")
	}
	r, _ := store.LoadReference(repoDir, "R-0001")
	if len(r.Facets) != 0 {
		t.Fatalf("reference mutated on invalid retag: %v", r.Facets)
	}
}

// TestReferenceRetagNoAxis: retag with neither --facet nor --component errors.
func TestReferenceRetagNoAxis(t *testing.T) {
	projDir, _, _ := setupProject(t)
	mkReference(t, projDir, "")
	if _, err := execCerne(t, "reference", "retag", "R-0001", "--dir", projDir); err == nil {
		t.Fatalf("retag with no axis should fail")
	}
}
