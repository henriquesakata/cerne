package index

import (
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestSearchFindsNoteByTag: a note's facet/component tags are indexed into its
// body, so a search by the tag surfaces the note even when the term never
// appears in its prose.
func TestSearchFindsNoteByTag(t *testing.T) {
	repoDir := t.TempDir()
	now := time.Now().UTC()
	n := store.Note{
		ID: "N-0001", Title: "Procedimento de instalação", State: domain.NoteActive,
		Facets: []string{"infra"}, Components: []string{"gitea"},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := store.SaveNote(repoDir, n); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}
	// Body deliberately omits "gitea": only the component tag carries it.
	writeContent(t, repoDir, "content/notes/N-0001.md",
		"---\ncerne_id: N-0001\n---\n\n# Procedimento de instalação\n\nPassos do setup sem citar a ferramenta.\n")

	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, dbPath); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	results, err := Search(dbPath, "gitea", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].EntityID != "N-0001" {
		t.Fatalf("Search(gitea) = %+v, want N-0001 via component tag", results)
	}
}
