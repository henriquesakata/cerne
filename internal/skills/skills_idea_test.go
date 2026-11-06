package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsIdeas pins F-0312 crit. 13: the canonical
// authoring skill exposes the idea verbs, the rule "an idea is not a demand"
// and the on-demand policy for agents (crit. 9).
func TestCerneAuthoringDocumentsIdeas(t *testing.T) {
	c := All()[0].Content
	for _, sub := range []string{
		"cerne idea new",
		"cerne idea list",
		"cerne idea show",
		"cerne show I-NNNN",
		"cerne idea drop",
		"cerne idea promote",
		"not a demand",
		"--search",
		"on demand",
		"never pull ideas in unasked",
	} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}
