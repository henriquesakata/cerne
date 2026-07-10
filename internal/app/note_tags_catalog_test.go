package app

import (
	"testing"
)

// TestNoteTagCatalog: the catalog aggregates facets and components across
// ACTIVE notes only (a superseded note's tags are history), counting each
// distinct tag once per note that carries it.
func TestNoteTagCatalog(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)

	mustNote := func(title string, facets, components []string) string {
		t.Helper()
		n, err := CreateNote(repoDir, projDir, title, "corpo real da nota", nil, facets, components)
		if err != nil {
			t.Fatalf("CreateNote(%s): %v", title, err)
		}
		return n.ID
	}

	mustNote("A", []string{"infra"}, []string{"gitea"})
	mustNote("B", []string{"infra", "code"}, []string{"gitea", "kpack"})
	mustNote("C", nil, []string{"convention"})
	old := mustNote("D", []string{"process"}, []string{"legacy"})
	if _, err := TransitionNote(repoDir, projDir, old, "", "obsolete"); err != nil {
		t.Fatalf("TransitionNote: %v", err)
	}

	cat, err := NoteTagCatalog(repoDir)
	if err != nil {
		t.Fatalf("NoteTagCatalog: %v", err)
	}

	wantFacets := map[string]int{"infra": 2, "code": 1}
	if len(cat.Facets) != len(wantFacets) {
		t.Fatalf("facets = %v, want %v", cat.Facets, wantFacets)
	}
	for f, n := range wantFacets {
		if cat.Facets[f] != n {
			t.Errorf("facet %s = %d, want %d", f, cat.Facets[f], n)
		}
	}

	wantComponents := map[string]int{"gitea": 2, "kpack": 1, "convention": 1}
	if len(cat.Components) != len(wantComponents) {
		t.Fatalf("components = %v, want %v", cat.Components, wantComponents)
	}
	for c, n := range wantComponents {
		if cat.Components[c] != n {
			t.Errorf("component %s = %d, want %d", c, cat.Components[c], n)
		}
	}
}

// TestNoteTagCatalogEmpty: no notes yields an empty (non-nil) catalog, not an error.
func TestNoteTagCatalogEmpty(t *testing.T) {
	repoDir, _, _ := newFeatureBackend(t)
	cat, err := NoteTagCatalog(repoDir)
	if err != nil {
		t.Fatalf("NoteTagCatalog: %v", err)
	}
	if len(cat.Facets) != 0 || len(cat.Components) != 0 {
		t.Fatalf("catalog = %+v, want empty", cat)
	}
	if cat.Facets == nil || cat.Components == nil {
		t.Fatalf("catalog maps must be non-nil for stable JSON ({} not null)")
	}
}
