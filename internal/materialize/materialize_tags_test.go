package materialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestMaterializeNoteTags: an active note's facets/components render in its
// CONTEXT.md "Conhecimento" bullet.
func TestMaterializeNoteTags(t *testing.T) {
	backend := seedBackend(t)
	n := store.Note{
		ID: "N-0001", Title: "Instalação do Gitea", State: domain.NoteActive,
		Facets: []string{"infra"}, Components: []string{"gitea", "kpack"},
		CreatedAt: seedTime, UpdatedAt: seedTime,
	}
	if err := store.SaveNote(backend, n); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}
	dir := filepath.Join(backend, "content", "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "N-0001.md"),
		[]byte("---\ncerne_id: N-0001\n---\n\nPassos.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", false); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	ctx := readContext(t, docsDir)
	if !strings.Contains(ctx, "facet infra") || !strings.Contains(ctx, "comp gitea,kpack") {
		t.Fatalf("CONTEXT.md missing note tags:\n%s", ctx)
	}
}
