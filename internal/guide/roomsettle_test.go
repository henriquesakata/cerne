package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// Asking why room watch --once woke on the first message of a burst lands on
// the FAQ entry that teaches --settle.
// E-0006 crit. 4
// T-0020 crit. 3
func TestGuideAnswersRoomWatchSettle(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	for _, q := range []string{"room watch --once wakes me on the first message of a burst", "room watch settle"} {
		got := guide.Search(corpus, q, 3)
		if len(got) == 0 {
			t.Fatalf("%q: no result", q)
		}
		first := got[0]
		if first.Source != "faq" || !strings.Contains(first.Title, "burst") {
			t.Fatalf("%q: first result is %q (%s)", q, first.Title, first.Source)
		}
		for _, want := range []string{"--settle", "1.5s", "5s", "--settle 0", "close"} {
			if !strings.Contains(first.Text, want) {
				t.Errorf("%q: entry lacks %q", q, want)
			}
		}
	}
}
