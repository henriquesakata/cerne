package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsStageShowAndAmend pins F-0338 in the authoring
// skill: the reading part says where the real state is, that show prints the
// document of the stage and how to read the others (criterion 16); the
// approved-spec part says an amendment is written into the text through
// `feature amend --doc spec|plan`, never appended; and the result amend is no
// longer described as the only document amend.
func TestCerneAuthoringDocumentsStageShowAndAmend(t *testing.T) {
	c := fullAuthoring(t)
	for _, sub := range []string{
		"the real state is line 1 of `cerne show`",
		"a status line in the body is history",
		"prints the document of its stage",
		"`--doc spec|plan|result` picks another",
		"`--all` brings them all",
		"`cerne feature amend F-NNNN --doc spec|plan",
		"never appended",
		"`cerne show F-NNNN --doc spec --rev N`",
	} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
	if strings.Contains(c, "(never spec or plan)") {
		t.Error("skill still says the amend never touches the spec or plan")
	}
}
