package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsClarifyExpect pins F-0330: the skill documents
// clarify answer's --expect guard, the key the approve refusal prints.
func TestCerneAuthoringDocumentsClarifyExpect(t *testing.T) {
	c := All()[0].Content
	for _, sub := range []string{"--expect <key>", "numbering moved", "nothing is written"} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}
