package guide_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// F-0483 crit. 4, 5: asking about a note or a reference lands on the FAQ
// entry that teaches the split, in English.
func TestGuideAnswersNoteOrReference(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	for _, q := range []string{"note or reference", "should this go in a note or a reference"} {
		got := guide.Search(corpus, q, 3)
		if len(got) == 0 {
			t.Fatalf("%q: no result", q)
		}
		first := got[0]
		if first.Source != "faq" || !strings.Contains(first.Title, "Note or reference?") {
			t.Fatalf("%q: first result is %q", q, first.Title)
		}
		for _, want := range []string{"cerne reference new", "--informs N-NNNN", "cerne reference link R-NNNN --informs N-NNNN"} {
			if !strings.Contains(first.Text, want) {
				t.Errorf("%q: entry lacks %q", q, want)
			}
		}
		if m := regexp.MustCompile(`(?i)[áàâãéêíóôõúç]|\b(nota|fonte|orientação|não|para|uma|com)\b`).FindString(first.Title + first.Text); m != "" {
			t.Errorf("entry has Portuguese %q", m)
		}
	}
}
