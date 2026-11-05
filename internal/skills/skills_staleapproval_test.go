package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsStaleApproval pins F-0318 in the authoring skill:
// an approved spec stays editable, a different spec stales the approval, the
// start is refused until a human re-approves with `cerne feature approve` on
// the approved feature itself, and show/next say so.
func TestCerneAuthoringDocumentsStaleApproval(t *testing.T) {
	c := All()[0].Content
	for _, sub := range []string{
		"stales the approval",
		"`cerne feature start` refuses",
		"re-approves",
		"`approval: stale`",
		"`approval_stale`",
	} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
	if strings.Contains(c, "Refused past `candidate`: an approved spec changes on the feature page") {
		t.Error("skill still says feature spec refuses an approved feature")
	}
}
