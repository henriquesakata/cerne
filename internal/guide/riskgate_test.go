package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// Asking what the risk gate does, or whether the agent may merge or push a
// card through it, lands on the FAQ entry of accept = "risk": the three
// conditions, the human merge for the rest, and the two things an agent never
// does. The answer fits the guide's cut.
// F-0516 crit. 13
func TestGuideAnswersTheRiskGate(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	wants := []string{`accept = "risk"`, "`gates.risk_human_paths`", "`gates.risk_human_facets`", "win", "marked test", "--merged", "never merges", "never forces"}
	for _, q := range []string{
		"what does accept = risk do",
		"which deliveries does the risk gate accept on its own",
		"why did complete hold my card for the human merge under the risk gate",
	} {
		got := guide.Search(corpus, q, 3)
		if len(got) == 0 {
			t.Fatalf("%q: no result", q)
		}
		first := got[0]
		if first.Source != "faq" {
			t.Fatalf("%q: first result is %q (%s), want an FAQ entry", q, first.Title, first.Source)
		}
		for _, want := range wants {
			if !strings.Contains(first.Text, want) {
				t.Errorf("%q: first entry %q lacks %q", q, first.Title, want)
			}
		}
		if strings.HasSuffix(first.Text, "…") {
			t.Errorf("%q: the answer is cut at the guide's limit", q)
		}
	}
}
