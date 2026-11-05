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
		"WAITING FOR RE-APPROVAL",
	} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
	if strings.Contains(c, "Refused past `candidate`: an approved spec changes on the feature page") {
		t.Error("skill still says feature spec refuses an approved feature")
	}
	// The normal path is the authoring loop; the Backlog chat is not presented
	// as one (it reaches an approved spec only from a refine conversation that
	// already existed).
	if !strings.Contains(c, "edit `.docs/current/features/F-NNNN-<slug>/spec.md` and `cerne push`") {
		t.Error("skill does not name .docs/ + cerne push as the normal path for an approved spec")
	}
	if strings.Contains(c, "the feature page, the Backlog chat) — no revert") {
		t.Error("skill presents the Backlog chat as a normal path for an approved spec")
	}
	// Criterion 14: an approval from before F-0318 records its baseline.
	if !strings.Contains(c, "records the current spec as its baseline") {
		t.Error("skill does not say a legacy approval records its baseline on the next approve")
	}
}
