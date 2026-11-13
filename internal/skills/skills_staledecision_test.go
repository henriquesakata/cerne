package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsStaleDecision pins F-0385: the authoring skill
// cites the stale-decision rule and its two new exits, keep and retire.
func TestCerneAuthoringDocumentsStaleDecision(t *testing.T) {
	c := fullAuthoring(t)
	for _, sub := range []string{
		"stale-decision",
		"cerne decision keep D-NNNN",
		`cerne decision retire D-NNNN --reason "…"`,
		"rejected or superseded",
		"lint.stale_decision_days",
	} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}
