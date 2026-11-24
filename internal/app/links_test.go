package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// linksRepo builds a backend with features F-0001 (cites the cache ADR in
// prose), F-0002 (already links D-0002 with a wiki-link) and decisions
// D-0001..D-0003; the queue already holds a rejected F-0001 → D-0003.
func linksRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	now := time.Now()
	for _, f := range []store.Feature{
		{ID: "F-0001", Title: "Cache layer", Module: "core", State: domain.StateDone, CreatedAt: now, UpdatedAt: now},
		{ID: "F-0002", Title: "Other", Module: "core", State: domain.StateDone, CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.SaveFeature(repo, f); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []store.Decision{
		{ID: "D-0001", Title: "Cache ADR", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now},
		{ID: "D-0002", Title: "Other ADR", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now},
		{ID: "D-0003", Title: "Third ADR", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now},
	} {
		if err := store.SaveDecision(repo, d); err != nil {
			t.Fatal(err)
		}
	}
	writeDoc(t, repo, "content/features/F-0001/spec.md", "# Cache\n\nWe keep it in memory,\nas decided in the cache ADR.\n")
	writeDoc(t, repo, "content/features/F-0002/spec.md", "# Other\n\nSee [[D-0002]] and the cache ADR too.\n")
	err := store.SaveLinkSuggestions(repo, []store.LinkSuggestion{{N: 1, From: "F-0001", To: "D-0003", Excerpt: "x", Status: store.LinkRejected}})
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

// TestFilterLinkSuggestions: one case per drop reason plus one that passes.
func TestFilterLinkSuggestions(t *testing.T) { // F-0502 crit. 4, 7
	cases := []struct {
		name   string
		raw    RawLinkSuggestion
		reason string // "" = kept
	}{
		{"kept, whitespace normalized", RawLinkSuggestion{"F-0001", "D-0001", "decided in the   cache ADR"}, ""},
		{"unknown source", RawLinkSuggestion{"F-9999", "D-0001", "cache ADR"}, LinkDropUnknownSource},
		{"unknown target", RawLinkSuggestion{"F-0001", "D-9999", "cache ADR"}, LinkDropUnknownTarget},
		{"self link", RawLinkSuggestion{"F-0001", "F-0001", "cache ADR"}, LinkDropSelf},
		{"excerpt not in the source", RawLinkSuggestion{"F-0001", "D-0001", "the redis ADR"}, LinkDropExcerpt},
		{"excerpt under 12 runes", RawLinkSuggestion{"F-0001", "D-0001", "cache  ADR"}, LinkDropShort}, // the importer's minimum
		{"empty excerpt", RawLinkSuggestion{"F-0001", "D-0001", "  "}, LinkDropShort},
		{"edge already in the graph", RawLinkSuggestion{"F-0002", "D-0002", "the cache ADR"}, LinkDropEdge},
		{"pair already in the queue", RawLinkSuggestion{"F-0001", "D-0003", "cache ADR"}, LinkDropQueued},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kept, dropped, err := FilterLinkSuggestions(linksRepo(t), []RawLinkSuggestion{tc.raw})
			if err != nil {
				t.Fatal(err)
			}
			if tc.reason == "" {
				if len(kept) != 1 || len(dropped) != 0 {
					t.Fatalf("kept = %v dropped = %v, want kept", kept, dropped)
				}
				return
			}
			if len(kept) != 0 || !reflect.DeepEqual(dropped, map[string]int{tc.reason: 1}) {
				t.Fatalf("kept = %v dropped = %v, want dropped for %s", kept, dropped, tc.reason)
			}
		})
	}
	t.Run("duplicate in the same batch", func(t *testing.T) {
		a := RawLinkSuggestion{"F-0001", "D-0001", "in the cache ADR"}
		kept, dropped, err := FilterLinkSuggestions(linksRepo(t), []RawLinkSuggestion{a, a})
		if err != nil {
			t.Fatal(err)
		}
		if len(kept) != 1 || dropped[LinkDropDuplicate] != 1 {
			t.Fatalf("kept = %v dropped = %v", kept, dropped)
		}
	})
}
