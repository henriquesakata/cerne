package skills

import (
	"strings"
	"testing"
)

// TestSkillRunbookSectionTeachesDeclaredStepErrata pins F-0345 crit. 8: the
// runbook section teaches --step (with a two-step example), --no-step,
// show --consolidated and runbook merge.
func TestSkillRunbookSectionTeachesDeclaredStepErrata(t *testing.T) {
	_, sec, ok := strings.Cut(fullAuthoring(t), "## Runbooks (operational procedures)")
	if !ok {
		t.Fatal("skill has no Runbooks section")
	}
	sec, _, _ = strings.Cut(sec, "\n## Runbook structure")
	for _, sub := range []string{
		"cerne runbook errata RB-NNNN --step S6.6,S6.7",
		"--no-step",
		"cerne show RB-NNNN --consolidated",
		"cerne runbook merge RB-NNNN",
	} {
		if !strings.Contains(sec, sub) {
			t.Errorf("Runbooks section missing %q", sub)
		}
	}
}
