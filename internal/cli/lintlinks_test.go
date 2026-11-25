package cli

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/lint"
)

// TestTaskActionForTheLinksQueue: a finding on links/suggestions.json is fixed
// by a command or an instruction, never by an errata. Rejecting is a human act,
// so the task asks the owner; repairing the file is an instruction, not a command.
func TestTaskActionForTheLinksQueue(t *testing.T) { // F-0502 crit. 8
	for _, c := range []struct{ fix, want string }{
		{"a person runs: cerne links reject 4", "ask the project owner to run: cerne links reject 4"},
		{"repair links/suggestions.json", "repair links/suggestions.json"},
	} {
		got := taskAction(lint.Finding{Rule: lint.RuleRefToSuperseded, File: "links/suggestions.json", Fix: c.fix})
		if got != c.want {
			t.Errorf("taskAction(%q) = %q, want %q", c.fix, got, c.want)
		}
	}
}
