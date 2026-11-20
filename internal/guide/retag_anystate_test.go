package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
)

// TestGuideSaysTagsChangeInAnyState (F-0482 criterion 8): the first section for
// "can I retag a closed feature" answers yes and points to errata for the body,
// and no section of the guide still teaches the old refusal.
func TestGuideSaysTagsChangeInAnyState(t *testing.T) {
	corpus := guide.Corpus(cli.Root())
	got := guide.Search(corpus, "can I retag a closed feature", 1)
	if len(got) != 1 {
		t.Fatalf("no section for the query")
	}
	for _, want := range []string{"any state", "errata", "cerne idea retag", "cerne pending retag"} {
		if !strings.Contains(got[0].Text, want) {
			t.Errorf("first section (%s) does not say %q:\n%s", got[0].Title, want, got[0].Text)
		}
	}
	for _, s := range corpus {
		if strings.Contains(s.Text, "retag only applies") || strings.Contains(s.Text, "frozen history") && strings.Contains(s.Text, "retag") {
			t.Errorf("section %q (%s) still teaches the retag refusal", s.Title, s.Source)
		}
	}
}
