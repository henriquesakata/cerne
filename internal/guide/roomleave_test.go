package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// F-0511 crit. 7: asking how to leave a war room lands on the FAQ entry that
// names `cerne room leave`, says the capability stops working and that
// closing a tab does not leave.
func TestGuideAnswersRoomLeave(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	for _, q := range []string{"how do I leave a war room and stop my session from working?", "room leave"} {
		got := guide.Search(corpus, q, 3)
		if len(got) == 0 {
			t.Fatalf("%q: no result", q)
		}
		first := got[0]
		if first.Source != "faq" || !strings.Contains(first.Title, "leave a war room") {
			t.Fatalf("%q: first result is %q", q, first.Title)
		}
		for _, want := range []string{"`cerne room leave <room>`", "stops working", "does not leave", "`cerne room read <room>`"} {
			if !strings.Contains(first.Text, want) {
				t.Errorf("%q: entry lacks %q", q, want)
			}
		}
	}
}
