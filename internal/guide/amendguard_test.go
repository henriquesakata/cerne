package guide_test

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/guide"
	"github.com/henriquesakata/cerne/internal/skills"
)

// amendLineBefore is the size of SKILL.md's amend line before F-0485, which
// may grow by at most 300 bytes (criterion 9).
const amendLineBefore = 199

// TestGuideTeachesTheAmendGuard (F-0485 criterion 9): the short guide's amend
// line says an amend in development holds at once and that the command refuses
// another session's card, within 300 bytes; `cerne guide` answers the rule.
func TestGuideTeachesTheAmendGuard(t *testing.T) {
	var line string
	for _, l := range strings.Split(skills.All()[0].Content, "\n") {
		if strings.Contains(l, "cerne feature amend F-NNNN --doc spec|plan") {
			line = l
		}
	}
	for _, want := range []string{"in development", "at once", "another session"} {
		if !strings.Contains(strings.ToLower(line), want) {
			t.Errorf("SKILL.md amend line does not say %q:\n%s", want, line)
		}
	}
	if n := len(line); n > amendLineBefore+300 {
		t.Errorf("SKILL.md amend line is %d bytes, at most %d", n, amendLineBefore+300)
	}

	got := guide.Search(guide.Corpus(cli.Root()), "amend in development", 1)
	if len(got) != 1 {
		t.Fatal("no section for the query")
	}
	for _, want := range []string{"takes effect at once", "no re-approval", "another session", "--force"} {
		if !strings.Contains(got[0].Text, want) {
			t.Errorf("first section (%s) does not say %q:\n%s", got[0].Title, want, got[0].Text)
		}
	}
}
