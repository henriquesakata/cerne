package importer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

// seedImportedDoc writes a content doc stamped as imported from source, with
// no entity file behind it (a doc imported by the case-folder convention, or an
// orphan).
func seedImportedDoc(t *testing.T, repo, source string) {
	t.Helper()
	notePath := filepath.Join(repo, "content", "notes", "N-0001.md")
	if err := os.MkdirAll(filepath.Dir(notePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notePath, []byte("---\ncerne_id: N-0001\nimported_from: "+source+"\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFilterPlanItems(t *testing.T) {
	repo := t.TempDir()
	if err := store.InitLayout(repo, store.Project{Name: "t"}); err != nil {
		t.Fatal(err)
	}
	// seed: an orphan content doc already imported from CLAUDE.md: no entity
	// loads, so the whole source counts as imported (F-0500, conservative).
	seedImportedDoc(t, repo, "CLAUDE.md")
	now := time.Now().UTC()
	// seed: a registered feature titled "Foo"
	if err := store.SaveFeature(repo, store.Feature{ID: "F-0001", Title: "Foo", State: "implemented", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	items := []PlanItem{
		{Kind: "note", Title: "A", State: "active", Body: "b", Source: "CLAUDE.md"},                          // already imported → skip
		{Kind: "note", Title: "B", State: "active", Body: "b", Source: "CLAUDE.md"},                          // orphan doc: same source → skip, never duplicate
		{Kind: "feature", Title: "Case 0001 — Foo", State: "implemented", Body: "b", Source: "HISTORY.md#1"}, // title registered → skip
		{Kind: "feature", Title: "New Thing", State: "implemented", Body: "b", Source: "HISTORY.md#2"},       // kept
	}
	kept, skipped, err := FilterPlanItems(repo, items)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 3 || len(kept) != 1 || kept[0].Title != "New Thing" {
		t.Fatalf("FilterPlanItems kept=%+v skipped=%d, want 1 kept (New Thing), 3 skipped", kept, skipped)
	}
}

// F-0500 crit. 7: with a real entity behind the doc, only its own title is
// skipped; a new title from the same source is kept.
func TestFilterPlanItemsSameSourceNewTitle(t *testing.T) {
	repo := t.TempDir()
	if err := store.InitLayout(repo, store.Project{Name: "t"}); err != nil {
		t.Fatal(err)
	}
	seedImportedDoc(t, repo, "CLAUDE.md")
	now := time.Now().UTC()
	if err := store.SaveNote(repo, store.Note{ID: "N-0001", Title: "A", State: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	items := []PlanItem{
		{Kind: "note", Title: "A", State: "active", Body: "b", Source: "CLAUDE.md"},
		{Kind: "note", Title: "B", State: "active", Body: "b", Source: "CLAUDE.md"},
	}
	kept, skipped, err := FilterPlanItems(repo, items)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 || len(kept) != 1 || kept[0].Title != "B" {
		t.Fatalf("kept=%+v skipped=%d, want only B kept", kept, skipped)
	}
}

// Declared limit (F-0500): the key is source + title, so a re-mapping of the
// same file with rewritten titles imports the items again. The dry-run shows
// the duplicate before --yes. Features have the F-0045 title guard on top.
func TestFilterPlanItemsRewrittenTitleIsNotDeduped(t *testing.T) {
	repo := t.TempDir()
	if err := store.InitLayout(repo, store.Project{Name: "t"}); err != nil {
		t.Fatal(err)
	}
	seedImportedDoc(t, repo, "CLAUDE.md")
	now := time.Now().UTC()
	if err := store.SaveNote(repo, store.Note{ID: "N-0001", Title: "Project state", State: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	items := []PlanItem{{Kind: "note", Title: "Current project state", State: "active", Body: "b", Source: "CLAUDE.md"}}
	kept, skipped, err := FilterPlanItems(repo, items)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(kept) != 1 {
		t.Fatalf("kept=%+v skipped=%d, want the rewritten title kept (declared limit)", kept, skipped)
	}
}

// F-0500 crit. 7: the key includes the kind, so a decision and a note with the
// same source and title do not mask each other.
func TestFilterPlanItemsKindIsPartOfTheKey(t *testing.T) {
	repo := t.TempDir()
	if err := store.InitLayout(repo, store.Project{Name: "t"}); err != nil {
		t.Fatal(err)
	}
	seedImportedDoc(t, repo, "CLAUDE.md")
	now := time.Now().UTC()
	if err := store.SaveNote(repo, store.Note{ID: "N-0001", Title: "Same", State: "active", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	items := []PlanItem{
		{Kind: "note", Title: "Same", State: "active", Body: "b", Source: "CLAUDE.md"},
		{Kind: "decision", Title: "Same", State: "active", Body: "b", Source: "CLAUDE.md"},
	}
	kept, skipped, err := FilterPlanItems(repo, items)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 || len(kept) != 1 || kept[0].Kind != "decision" {
		t.Fatalf("kept=%+v skipped=%d, want only the decision kept", kept, skipped)
	}
}
