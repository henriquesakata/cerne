package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// Asking whether an agent may approve or accept on its own lands on the FAQ
// entry of the gate settings: both keys, the owner's, never the agent's.
// F-0515 crit. 11
func TestGuideAnswersTheGateSettings(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	for _, q := range []string{"can an agent approve or accept on its own", "gates.approve auto", "approved without human review"} {
		got := guide.Search(corpus, q, 3)
		if len(got) == 0 {
			t.Fatalf("%q: no result", q)
		}
		first := got[0]
		if first.Source != "faq" {
			t.Fatalf("%q: first result is %q (%s), want the FAQ entry", q, first.Title, first.Source)
		}
		for _, want := range []string{"`gates.approve`", "`gates.accept`", "owner", "never changes them"} {
			if !strings.Contains(first.Text, want) {
				t.Errorf("%q: first entry %q lacks %q", q, first.Title, want)
			}
		}
	}
}
