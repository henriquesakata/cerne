package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringPointsNextAfterComplete pins F-0371: the skill names
// `cerne next` as the source of the step left after complete.
func TestCerneAuthoringPointsNextAfterComplete(t *testing.T) {
	c := All()[0].Content
	for _, sub := range []string{"FINISH DELIVERY", "WAITING FOR HUMAN ACCEPT", "`cerne next F-NNNN` is the source of the next step", "the `<url>` is the merge request link you fill in"} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}
