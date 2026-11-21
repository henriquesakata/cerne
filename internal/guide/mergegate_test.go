package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// Asking whether the PR merge can accept the card lands on the FAQ entry of
// the merge gate, which teaches the three facts an agent needs.
// F-0488 crit. 12
func TestGuideAnswersTheMergeGate(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	for _, q := range []string{"accept on merge", "can merging the PR accept the feature"} {
		got := guide.Search(corpus, q, 3)
		if len(got) == 0 {
			t.Fatalf("%q: no result", q)
		}
		first := got[0]
		if first.Source != "faq" || !strings.Contains(first.Title, "merge") {
			t.Fatalf("%q: first result is %q (%s)", q, first.Title, first.Source)
		}
		for _, want := range []string{`accept = "merge"`, "cerne feature accept --merged", "never merges"} {
			if !strings.Contains(first.Text, want) {
				t.Errorf("%q: entry lacks %q", q, want)
			}
		}
	}
}
