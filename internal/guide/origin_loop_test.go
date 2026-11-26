package guide

import (
	"strings"
	"testing"
)

// B-0086 (review): `cerne guide "feature loop"` returns the lifecycle
// reference's Feature loop section; it must cite the origin choice too.
func TestGuideFeatureLoopCitesTheOriginChoice(t *testing.T) {
	got := Search(Corpus(testTree()), "feature loop", 3)
	if len(got) == 0 {
		t.Fatal("no section for \"feature loop\"")
	}
	seen := 0
	for _, s := range got {
		if !strings.Contains(s.Title, "Feature loop") {
			continue
		}
		seen++
		for _, want := range []string{"--on-behalf", "--origin agent"} {
			if !strings.Contains(s.Text, want) {
				t.Errorf("section %q (%s) lacks %q", s.Title, s.Source, want)
			}
		}
	}
	if seen == 0 {
		t.Fatalf("no Feature loop section in %d results", len(got))
	}
}
