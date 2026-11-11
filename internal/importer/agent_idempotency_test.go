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
	// seed: a registered feature titled "Foo"
	now := time.Now().UTC()
	if err := store.SaveFeature(repo, store.Feature{ID: "F-0001", Title: "Foo", State: "implemented", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	items := []PlanItem{
		{Kind: "note", Title: "A", State: "active", Body: "b", Source: "CLAUDE.md"},                          // already imported → skip
		{Kind: "feature", Title: "Case 0001 — Foo", State: "implemented", Body: "b", Source: "HISTORY.md#1"}, // title registered → skip
		{Kind: "feature", Title: "New Thing", State: "implemented", Body: "b", Source: "HISTORY.md#2"},       // kept
	}
	kept, skipped, err := FilterPlanItems(repo, items)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 2 || len(kept) != 1 || kept[0].Title != "New Thing" {
		t.Fatalf("FilterPlanItems kept=%+v skipped=%d, want 1 kept (New Thing), 2 skipped", kept, skipped)
	}
}
