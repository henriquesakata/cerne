package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// F-0503 crit. 2, 7, 10: asking how to keep an archived copy lands on the FAQ
// entry that says reference new uses the network when the setting is on, that
// the copy is the nearest to the registration day and that --opt-out exists.
func TestGuideAnswersReferenceArchive(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	for _, q := range []string{"reference archive", "how do I keep an archived copy of a source"} {
		got := guide.Search(corpus, q, 3)
		if len(got) == 0 {
			t.Fatalf("%q: no result", q)
		}
		first := got[0]
		if first.Source != "faq" || !strings.Contains(first.Title, "archived copy") {
			t.Fatalf("%q: first result is %q", q, first.Title)
		}
		for _, want := range []string{"`cerne reference new` also uses the network", "nearest the day", "--opt-out"} {
			if !strings.Contains(first.Text, want) {
				t.Errorf("%q: entry lacks %q", q, want)
			}
		}
	}
}
