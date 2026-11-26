package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// Asking how the owner tells the risk gate it was wrong, or whether the
// agent may answer it or apply a proposal, lands on the FAQ entry of the
// feedback: the command, the calibration, the proposals, and that both are
// the owner's — the agent never writes feedback nor applies a proposal to
// pass a delivery. The answer fits the guide's cut.
// F-0518 crit. 11
func TestGuideAnswersTheRiskFeedback(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	wants := []string{"cerne feature feedback", "--should-have-asked", "--not-needed", "gates.risk_calibration", "GATE PROPOSALS", "--accept-mode", "owner's", "never writes feedback", "never applies a proposal"}
	for _, q := range []string{
		"how does the owner tell the risk gate it should have asked",
		"can the agent answer the risk gate feedback or apply a gate proposal",
		"why does the risk gate hold every delivery in calibration",
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
