package cli

import (
	"testing"

	"gitlab.com/rockt/cerne/internal/store"
)

// TestNoteRetagSetsTags: retag sets both axes on an existing note.
func TestNoteRetagSetsTags(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "note", "new", "Gitea gotcha", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}
	out, err := execCerne(t, "note", "retag", "N-0001", "--facet", "infra", "--component", "Gitea", "--dir", projDir)
	if err != nil {
		t.Fatalf("note retag: %v\n%s", err, out)
	}
	n, _ := store.LoadNote(repoDir, "N-0001")
	if len(n.Facets) != 1 || n.Facets[0] != "infra" || len(n.Components) != 1 || n.Components[0] != "gitea" {
		t.Fatalf("retag persisted = %v / %v", n.Facets, n.Components)
	}
}

// TestNoteRetagPartialKeepsFacet: retagging only --component leaves facets intact
// (flag.Changed semantics).
func TestNoteRetagPartialKeepsFacet(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "note", "new", "T", "--facet", "infra", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}
	if _, err := execCerne(t, "note", "retag", "N-0001", "--component", "gitea", "--dir", projDir); err != nil {
		t.Fatalf("note retag: %v", err)
	}
	n, _ := store.LoadNote(repoDir, "N-0001")
	if len(n.Facets) != 1 || n.Facets[0] != "infra" {
		t.Fatalf("facet not preserved: %v", n.Facets)
	}
	if len(n.Components) != 1 || n.Components[0] != "gitea" {
		t.Fatalf("component = %v", n.Components)
	}
}

// TestNoteRetagInvalidFacet: an unknown facet fails the command and leaves the
// note unchanged.
func TestNoteRetagInvalidFacet(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "note", "new", "T", "--dir", projDir); err != nil {
		t.Fatalf("note new: %v", err)
	}
	if _, err := execCerne(t, "note", "retag", "N-0001", "--facet", "prod", "--dir", projDir); err == nil {
		t.Fatalf("retag --facet prod should fail")
	}
	n, _ := store.LoadNote(repoDir, "N-0001")
	if len(n.Facets) != 0 {
		t.Fatalf("note mutated on invalid retag: %v", n.Facets)
	}
}
