package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsHandoff pins the F-0339 rule: before ending a
// session that leaves work for another, `cerne work handoff`; the session that
// receives a brief naming a W- starts with `cerne work start … --from <W-id>`.
func TestCerneAuthoringDocumentsHandoff(t *testing.T) {
	c := All()[0].Content
	for _, sub := range []string{"cerne work handoff", "--from <W-id>"} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}
