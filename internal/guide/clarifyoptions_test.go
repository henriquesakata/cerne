package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// F-0517 crit. 7: asking how to write a clarification lands on the FAQ entry
// with the options form, the recommended suffix in both languages and the
// rule that a question with no possible answers stays without a pipe.
func TestGuideAnswersClarifyOptions(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	for _, q := range []string{"how do I write a clarification with options and a recommended answer?", "clarify recommended option"} {
		got := guide.Search(corpus, q, 3)
		if len(got) == 0 {
			t.Fatalf("%q: no result", q)
		}
		first := got[0]
		if first.Source != "faq" || !strings.Contains(first.Title, "clarification") {
			t.Fatalf("%q: first result is %q", q, first.Title)
		}
		for _, want := range []string{"(recomendada)", "(recommended)", "one per pipe", "without a pipe", "`cerne clarify list`"} {
			if !strings.Contains(first.Text, want) {
				t.Errorf("%q: entry lacks %q", q, want)
			}
		}
	}
}
