package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsWhenTestsSuffice (F-0390 crit. 5, as amended):
// the authoring skill carries the template's rule — when the Gauntlet pays,
// when the tests suffice and the form to write — says the approve check shows
// the project's own measured cost, and keeps cerne's batch numbers only as a
// labelled example: the skill ships to every project, so no note of cerne's.
func TestCerneAuthoringDocumentsWhenTestsSuffice(t *testing.T) {
	c := fullAuthoring(t)
	for _, want := range []string{
		"of behaviour, of contract, of persisted format, or of UI with a design choice",
		"a single, local effect, or a fix with a regression test",
		"`Barra: não aplicável (<por que os testes bastam>)`",
		"shows the Gauntlet cost measured in your own project",
		"For example, in cerne's own batch F-0368..F-0386, critic and judge took ~8% of a feature's weighted cost, up to ~22% on the small ones",
		"`gauntlet-cost`", "`light-with-bar`", "`standard-without-bar`",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("skill missing %q", want)
		}
	}
	if strings.Contains(c, "N-0056") {
		t.Error("skill cites N-0056, a note only cerne's backend holds")
	}
	if strings.Contains(c, "the loop's cost is not a reason to skip it —") {
		t.Error("skill still says the cost is never a reason to skip, against the rule")
	}
}
