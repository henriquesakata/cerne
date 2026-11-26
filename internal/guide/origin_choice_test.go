package guide

import (
	"strings"
	"testing"
)

// B-0086 (N-0060): asking how to record who asked for a card answers with the
// origin FAQ entry, which names both forms an agent must choose between.
func TestGuideTeachesTheOriginChoice(t *testing.T) {
	for _, q := range []string{"who asked for this card, how do I record the origin?", "on-behalf or origin agent when creating a card"} {
		got := Search(Corpus(testTree()), q, 3)
		if len(got) == 0 {
			t.Fatalf("no section for %q", q)
		}
		first := got[0]
		for _, want := range []string{"--on-behalf", "--origin agent", "must choose it"} {
			if !strings.Contains(first.Text, want) {
				t.Errorf("%q: first section %q lacks %q", q, first.Title, want)
			}
		}
	}
}
