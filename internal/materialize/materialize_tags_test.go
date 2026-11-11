package materialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
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

// TestMaterializeDecisionTags: an active decision's facets/components render in
// its CONTEXT.md decision line (mirrors notes).
func TestMaterializeDecisionTags(t *testing.T) {
	backend := seedBackend(t)
	d := store.Decision{
		ID: "D-0001", Title: "Escolha de IdP", State: domain.DecisionActive,
		Facets: []string{"auth"}, Components: []string{"keycloak"},
		CreatedAt: seedTime, UpdatedAt: seedTime,
	}
	if err := store.SaveDecision(backend, d); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	dir := filepath.Join(backend, "content", "decisions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "D-0001.md"),
		[]byte("---\ncerne_id: D-0001\n---\n\n## Contexto\n\nForças.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	docsDir := filepath.Join(t.TempDir(), ".docs")
	if _, err := Materialize(backend, docsDir, "", false); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	ctx := readContext(t, docsDir)
	if !strings.Contains(ctx, "facet auth") || !strings.Contains(ctx, "comp keycloak") {
		t.Fatalf("CONTEXT.md missing decision tags:\n%s", ctx)
	}
}
