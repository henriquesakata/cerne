package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// Asking whether an agent may approve or accept on its own lands on the FAQ
// entry of the gate settings: both keys, the owner's, never the agent's. Asking
// what the badge means lands on the entry of the marks, which says the mark
// follows the shell (B-0030). Each answer fits the guide's cut, so an agent
// reads all of it.
// F-0515 crit. 11
func TestGuideAnswersTheGateSettings(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	settings := []string{"`gates.approve`", "`gates.accept`", "owner", "never changes them"}
	marks := []string{"`gates.approve`", "agent's own `cerne feature accept`", "never marked", "CERNE_ACTOR=human", "approved: automatically (no human act)"}
	for q, wants := range map[string][]string{
		"can an agent approve or accept on its own":     settings,
		"how does the owner turn the approval gate off": settings,
		"approved without human review":                 marks,
		"which cards are marked accepted_via auto":      marks,
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
