package app

import (
	"errors"
	"strings"
	"testing"
)

// TestLoadLinkSourcesAndPrompt: the sources are the entities that have a
// content document (all, or the ids asked for), the catalog lists every
// entity, and the prompt carries the marker, the document and the catalog
// without the source itself.
func TestLoadLinkSourcesAndPrompt(t *testing.T) { // F-0502 crit. 3
	repo := linksRepo(t)
	srcs, catalog, err := LoadLinkSources(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 2 || srcs[0].ID != "F-0001" || srcs[1].ID != "F-0002" || srcs[0].Title != "Cache layer" {
		t.Fatalf("sources = %+v", srcs)
	}
	if len(catalog) != 5 {
		t.Fatalf("catalog = %+v, want the 5 entities", catalog)
	}
	one, _, err := LoadLinkSources(repo, []string{"F-0002"})
	if err != nil || len(one) != 1 || one[0].ID != "F-0002" {
		t.Fatalf("one = %+v, %v", one, err)
	}
	for _, bad := range []string{"F-9999", "D-0001"} { // unknown; known but with no document
		if _, _, err := LoadLinkSources(repo, []string{bad}); !errors.Is(err, ErrNoLinkSource) {
			t.Errorf("%s: err = %v, want ErrNoLinkSource", bad, err)
		}
	}
	p := BuildLinkPrompt(srcs[0], catalog)
	for _, want := range []string{LinkPromptMarker, "F-0001", "decided in the cache ADR", "D-0001 — Cache ADR", "D-0003 — Third ADR"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	if strings.Count(p, "F-0001 — Cache layer") != 1 { // only the header line
		t.Error("the source is in its own catalog")
	}
}

// TestEstimateLinkTokens: runes / 2 per prompt plus a fixed answer budget each.
func TestEstimateLinkTokens(t *testing.T) { // F-0502 crit. 3
	got := EstimateLinkTokens([]string{strings.Repeat("a", 400), strings.Repeat("é", 40)})
	if want := 200 + 20 + 2*LinkAnswerTokens; got != want {
		t.Errorf("estimate = %d, want %d", got, want)
	}
	if EstimateLinkTokens(nil) != 0 {
		t.Error("estimate of nothing != 0")
	}
}
