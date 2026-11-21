package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// Asking how a card goes back after the delivery lands on the FAQ entry of the
// revert steps, with the command, the drag and the merge-gate caveat.
// F-0499 crit. 5
func TestGuideAnswersRevertAfterDelivery(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	for _, q := range []string{"go back from Done to Ready", "card back to In development bug"} {
		got := guide.Search(corpus, q, 3)
		if len(got) == 0 {
			t.Fatalf("%q: no result", q)
		}
		first := got[0]
		if first.Source != "faq" || !strings.Contains(first.Title, "Done to Ready") {
			t.Fatalf("%q: first result is %q (%s)", q, first.Title, first.Source)
		}
		for _, want := range []string{"cerne feature revert <id> --reason", "drags the card", "not accepted again from the same PR"} {
			if !strings.Contains(first.Text, want) {
				t.Errorf("%q: entry lacks %q", q, want)
			}
		}
	}
}
