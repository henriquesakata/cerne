package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsPendings pins F-0322 crit. 16: the canonical
// authoring skill exposes the six pending verbs, the mandatory origin, the
// instruction to register pendings when closing a feature, and that a pending
// never blocks the lifecycle.
func TestCerneAuthoringDocumentsPendings(t *testing.T) {
	c := All()[0].Content
	for _, sub := range []string{
		"cerne pending new",
		"cerne pending list",
		"cerne pending show",
		"cerne show P-NNNN",
		"cerne pending resolve",
		"cerne pending drop",
		"cerne pending promote",
		"--from",
		"when you close a feature",
		"never blocks",
	} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}
