package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsWhenTestsSuffice (F-0390 crit. 5): the authoring
// skill carries the template's rule — when the Gauntlet pays, when the tests
// suffice and the form to write — with the measured cost, cites N-0056, and
// names the approve check's advice.
func TestCerneAuthoringDocumentsWhenTestsSuffice(t *testing.T) {
	c := All()[0].Content
	for _, want := range []string{
		"of behaviour, of contract, of persisted format, or of UI with a design choice",
		"a single, local effect, or a fix with a regression test",
		"`Barra: não aplicável (<por que os testes bastam>)`",
		"[[N-0056]]", "~8%", "20–22%",
		"`gauntlet-cost`", "`light-with-bar`", "`standard-without-bar`",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("skill missing %q", want)
		}
	}
	if strings.Contains(c, "the loop's cost is not a reason to skip it —") {
		t.Error("skill still says the cost is never a reason to skip, against the rule")
	}
}
