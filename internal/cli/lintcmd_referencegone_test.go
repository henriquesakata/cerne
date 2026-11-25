package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/lint"
)

// F-0501 crit. 7: a reference-gone task tells how to clear it.
func TestLintTaskActionReferenceGone(t *testing.T) {
	got := taskAction(lint.Finding{Rule: lint.RuleReferenceGone, Entity: "R-0001", Detail: "source gone"})
	if !strings.Contains(got, "cerne reference retire R-0001") || !strings.Contains(got, "cerne reference check R-0001") {
		t.Fatalf("task = %q", got)
	}
}
