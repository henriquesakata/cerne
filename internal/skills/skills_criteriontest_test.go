package skills

import (
	"strings"
	"testing"
)

// F-0473 crit. 12
func TestCerneAuthoringTeachesCriterionTestMarks(t *testing.T) {
	c := fullAuthoring(t)
	for _, sub := range []string{
		"// F-0123 crit. 2",
		"// B-0123",
		"(verificação manual)",
		"(manual verification)",
		"criterion-without-test",
		"orphan-criterion-test",
		"lint.criterion_tests",
		"test_roots",
	} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}
