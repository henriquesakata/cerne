package importer

import "testing"

func TestItemsToCandidatesFilters(t *testing.T) {
	items := []PlanItem{
		{Kind: "note", Title: "A", State: "active", Body: "b", Source: "CLAUDE.md"},
		{Kind: "feature", Title: "Case 0001 — Foo", State: "implemented", Body: "b", Source: "HISTORY.md#1"},
		{Kind: "feature", Title: "New Thing", State: "implemented", Body: "b", Source: "HISTORY.md#2"},
	}
	cands := itemsToCandidates(items)
	if len(cands) != 3 {
		t.Fatalf("itemsToCandidates len = %d, want 3", len(cands))
	}

	// imported_from guard: CLAUDE.md already imported → its item is dropped.
	kept, skipped := Unimported(cands, map[string]bool{"CLAUDE.md": true})
	if skipped != 1 || len(kept) != 2 {
		t.Fatalf("Unimported kept=%d skipped=%d, want kept=2 skipped=1", len(kept), skipped)
	}

	// title guard: a feature whose normalized title matches a registered feature
	// is dropped ("Case 0001 — Foo" normalizes to "Foo").
	kept2, skipped2 := DropRegisteredTitles(kept, map[string]bool{"Foo": true})
	if skipped2 != 1 || len(kept2) != 1 {
		t.Fatalf("DropRegisteredTitles kept=%d skipped=%d, want kept=1 skipped=1", len(kept2), skipped2)
	}
	if kept2[0].Title != "New Thing" {
		t.Fatalf("survivor = %q, want New Thing", kept2[0].Title)
	}
}
