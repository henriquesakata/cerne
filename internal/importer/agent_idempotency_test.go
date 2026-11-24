package importer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

func TestFilterPlanItems(t *testing.T) {
	repo := t.TempDir()
	if err := store.InitLayout(repo, store.Project{Name: "t"}); err != nil {
		t.Fatal(err)
	}
	// seed: a content doc already imported from CLAUDE.md (idempotency anchor)
	notePath := filepath.Join(repo, "content", "notes", "N-0001.md")
	if err := os.MkdirAll(filepath.Dir(notePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notePath, []byte("---\ncerne_id: N-0001\nimported_from: CLAUDE.md\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveNote(repo, store.Note{ID: "N-0001", Title: "A", State: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	// seed: a registered feature titled "Foo"
	if err := store.SaveFeature(repo, store.Feature{ID: "F-0001", Title: "Foo", State: "implemented", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	items := []PlanItem{
		{Kind: "note", Title: "A", State: "active", Body: "b", Source: "CLAUDE.md"},                          // already imported → skip
		{Kind: "feature", Title: "Case 0001 — Foo", State: "implemented", Body: "b", Source: "HISTORY.md#1"}, // title registered → skip
		{Kind: "feature", Title: "New Thing", State: "implemented", Body: "b", Source: "HISTORY.md#2"},       // kept
		{Kind: "note", Title: "B", State: "active", Body: "b", Source: "CLAUDE.md"},                          // same source, new title → kept (F-0500 crit. 7)
	}
	kept, skipped, err := FilterPlanItems(repo, items)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 2 || len(kept) != 2 || kept[0].Title != "New Thing" || kept[1].Title != "B" {
		t.Fatalf("FilterPlanItems kept=%+v skipped=%d, want 2 kept (New Thing, B), 2 skipped", kept, skipped)
	}
}
