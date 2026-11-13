package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsWorkRegistry pins the canonical authoring skill to
// the F-0130 crit. 8 rule: an agent picking up a non-feature task registers it
// with `cerne work start`, closes it with `cerne work done`, and checks
// `cerne work list` before delegating an error investigation to another agent
// (so duplicated work is visible before it happens).
func TestCerneAuthoringDocumentsWorkRegistry(t *testing.T) {
	c := fullAuthoring(t)

	contains := func(crit, sub string) {
		t.Helper()
		if !strings.Contains(c, sub) {
			t.Errorf("%s: skill missing %q", crit, sub)
		}
	}

	contains("crit8", "cerne work start")
	contains("crit8", "cerne work done")
	contains("crit8", "cerne work list")
}
