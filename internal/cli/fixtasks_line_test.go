package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/lint"
)

// F-0352 A4: the same target on two lines gives two distinct fix tasks.
func TestFixTasksCarryLine(t *testing.T) {
	d := "broken wiki-link [[N-9999]] — no such id: x, then 'cerne push'"
	var b bytes.Buffer
	printFixTasks(&b, []lint.Finding{
		{Rule: lint.RuleBrokenLink, File: "a.md", Line: 7, Detail: d},
		{Rule: lint.RuleBrokenLink, File: "a.md", Line: 13, Detail: d},
	})
	out := b.String()
	if !strings.Contains(out, "- [ ] a.md:7: fix ") || !strings.Contains(out, "- [ ] a.md:13: fix ") {
		t.Fatalf("tasks must carry file:line:\n%s", out)
	}
}
