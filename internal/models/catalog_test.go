package models

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestCatalogTwoLayers: criterion 2 — the global file wins per id (field by
// field, so a hand edit can carry only what it changes), the baseline fills the
// rest, and the result is the union of both id sets.
func TestCatalogTwoLayers(t *testing.T) {
	dir := t.TempDir()
	base := Baseline()
	if len(base.Entries) == 0 || base.DefaultModel == "" {
		t.Fatalf("baseline is empty: %+v", base)
	}
	demoted := base.Entries[0]
	global := `{"preferred_provider":"anthropic","entries":[
		{"id":"` + demoted.ID + `","visibility":"legacy"},
		{"id":"vendor-new-1","label":"Vendor New 1","provider":"vendor","efforts":["low"],"default_effort":"low"}
	]}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(global), 0o644); err != nil {
		t.Fatal(err)
	}

	cat, warn := Load(dir)
	if warn != nil {
		t.Fatalf("Load warn = %v, want nil", warn)
	}
	if len(cat.Entries) != len(base.Entries)+1 {
		t.Fatalf("entries = %d, want the union %d", len(cat.Entries), len(base.Entries)+1)
	}
	got, ok := cat.Find(demoted.ID)
	if !ok || got.Visibility != Legacy {
		t.Fatalf("global override lost: %+v", got)
	}
	if got.Label != demoted.Label || len(got.Efforts) != len(demoted.Efforts) {
		t.Fatalf("baseline fields not kept under a partial override: %+v", got)
	}
	added, ok := cat.Find("vendor-new-1")
	if !ok || added.Visibility != Current || added.Label != "Vendor New 1" {
		t.Fatalf("global-only entry = %+v, want it present as current", added)
	}
	if cat.PreferredProvider != "anthropic" || cat.DefaultModel != base.DefaultModel {
		t.Fatalf("document fields = %q/%q", cat.PreferredProvider, cat.DefaultModel)
	}
}

// TestCatalogFallsBackToBaseline: criterion 3 — an absent, empty or malformed
// global file serves the embedded baseline and reports why.
func TestCatalogFallsBackToBaseline(t *testing.T) {
	base := Baseline()
	cases := []struct {
		name    string
		content *string
		want    error
	}{
		{"absent", nil, fs.ErrNotExist},
		{"empty", ptr("  \n"), ErrEmpty},
		{"malformed json", ptr("{not json"), ErrMalformed},
		{"entry without id", ptr(`{"entries":[{"label":"x"}]}`), ErrMalformed},
		{"unknown visibility", ptr(`{"entries":[{"id":"x","visibility":"gone"}]}`), ErrMalformed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.content != nil {
				if err := os.WriteFile(filepath.Join(dir, FileName), []byte(*c.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cat, warn := Load(dir)
			if !errors.Is(warn, c.want) {
				t.Fatalf("warn = %v, want %v", warn, c.want)
			}
			if len(cat.Entries) != len(base.Entries) || cat.DefaultModel != base.DefaultModel {
				t.Fatalf("did not serve the baseline: %+v", cat)
			}
		})
	}
}

func ptr(s string) *string { return &s }
