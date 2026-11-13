package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsStaleDecision pins F-0385: the authoring skill
// cites the stale-decision rule and its two new exits, keep and retire.
func TestCerneAuthoringDocumentsStaleDecision(t *testing.T) {
	c := All()[0].Content
	for _, sub := range []string{
		"stale-decision",
		"cerne decision keep D-NNNN",
		`cerne decision retire D-NNNN --reason "…"`,
	} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}
