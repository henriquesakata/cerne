package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// Asking about the wait, or how the agent learns of the human's act, teaches
// that the move reaches the session at its next turn without the wait, and
// that the wait is still what wakes a stopped session.
// F-0504 crit. 7
func TestGuideTeachesCardNotices(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	for _, q := range []string{"feature wait", "how does the agent know when the human approved"} {
		got := guide.Search(corpus, q, 1)
		if len(got) == 0 {
			t.Fatalf("%q: no result", q)
		}
		for _, want := range []string{"card notice", "next turn", "wakes a stopped session"} {
			if !strings.Contains(got[0].Text, want) {
				t.Errorf("%q: first result %q (%s) lacks %q", q, got[0].Title, got[0].Source, want)
			}
		}
	}
}
