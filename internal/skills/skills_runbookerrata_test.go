package skills

import (
	"strings"
	"testing"
)

// TestSkillRunbookSectionTeachesDeclaredStepErrata pins F-0345 crit. 8: the
// runbook section teaches --altera (with a two-step example), --sem-passo,
// show --consolidado and runbook merge.
func TestSkillRunbookSectionTeachesDeclaredStepErrata(t *testing.T) {
	_, sec, ok := strings.Cut(All()[0].Content, "## Runbooks (operational procedures)")
	if !ok {
		t.Fatal("skill has no Runbooks section")
	}
	sec, _, _ = strings.Cut(sec, "\n## Runbook structure")
	for _, sub := range []string{
		"cerne runbook errata RB-NNNN --altera S6.6,S6.7",
		"--sem-passo",
		"cerne show RB-NNNN --consolidado",
		"cerne runbook merge RB-NNNN",
	} {
		if !strings.Contains(sec, sub) {
			t.Errorf("Runbooks section missing %q", sub)
		}
	}
}
