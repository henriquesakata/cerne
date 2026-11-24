package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/lint"
)

// TestTaskActionForTheLinksQueue: a finding on links/suggestions.json carries a
// command as its fix, not an errata entry; the task line says to run it.
func TestTaskActionForTheLinksQueue(t *testing.T) { // F-0502 crit. 8
	for _, fix := range []string{"cerne links reject 4", "repair links/suggestions.json"} {
		got := taskAction(lint.Finding{Rule: lint.RuleRefToSuperseded, File: "links/suggestions.json", Fix: fix})
		if strings.Contains(got, "errata") || !strings.Contains(got, fix) {
			t.Errorf("taskAction(%q) = %q", fix, got)
		}
	}
}
