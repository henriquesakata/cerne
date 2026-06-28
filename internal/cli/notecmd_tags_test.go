package cli

import (
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/store"
)

// TestNoteNewWithTags: --facet/--component are split, normalized and persisted.
func TestNoteNewWithTags(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	out, err := execCerne(t, "note", "new", "Gitea install gotcha",
		"--facet", "Infra,code", "--component", "Gitea,KPack", "--dir", projDir)
	if err != nil {
		t.Fatalf("note new --facet --component: %v\n%s", err, out)
	}
	n, err := store.LoadNote(repoDir, "N-0001")
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if len(n.Facets) != 2 || n.Facets[0] != "infra" || n.Facets[1] != "code" {
		t.Fatalf("facets = %v, want [infra code]", n.Facets)
	}
	if len(n.Components) != 2 || n.Components[0] != "gitea" || n.Components[1] != "kpack" {
		t.Fatalf("components = %v, want [gitea kpack]", n.Components)
	}
}

// TestNoteNewInvalidFacet: a facet outside the closed set fails the command and
// persists nothing.
func TestNoteNewInvalidFacet(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	out, err := execCerne(t, "note", "new", "T", "--facet", "prod", "--dir", projDir)
	if err == nil {
		t.Fatalf("note new --facet prod should fail, got:\n%s", out)
	}
	if !strings.Contains(out, "facet") {
		t.Fatalf("error output should name the offending facet:\n%s", out)
	}
	if notes, _ := store.LoadAllNotes(repoDir); len(notes) != 0 {
		t.Fatalf("invalid facet persisted %d note(s), want 0", len(notes))
	}
}
