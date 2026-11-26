package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// Asking who is listening in a war room lands on the FAQ entry that reads
// the `cerne room list` count and says what counts as listening.
// T-0021 crit. 2
// E-0006 crit. 8
func TestGuideAnswersRoomPresence(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	for _, q := range []string{"which war room participants are listening right now?", "who is idle in a war room"} {
		got := guide.Search(corpus, q, 3)
		if len(got) == 0 {
			t.Fatalf("%q: no result", q)
		}
		first := got[0]
		if first.Source != "faq" || !strings.Contains(first.Title, "listening") {
			t.Fatalf("%q: first result is %q", q, first.Title)
		}
		for _, want := range []string{"`cerne room list`", "(<L> listening)", "`cerne room watch <room> --once`", "idle", "does not count"} {
			if !strings.Contains(first.Text, want) {
				t.Errorf("%q: entry lacks %q", q, want)
			}
		}
	}
}
