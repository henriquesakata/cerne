package skills

import (
	"os"
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsLeanCLI pins F-0514 crit. 5: the embedded guide
// teaches --capability at card birth (SKILL.md and the lifecycle reference),
// the batch capability link and cerne next --all.
func TestCerneAuthoringDocumentsLeanCLI(t *testing.T) { // F-0514 crit. 5
	skill, err := os.ReadFile("cerne-authoring/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(skill), "its capability (`--capability C-…`)") {
		t.Errorf("SKILL.md does not teach --capability at card birth")
	}
	c := fullAuthoring(t)
	for _, sub := range []string{
		"also on `epic new` and `task new`",
		"comma-separated batch of cards",
		"`cerne next --all`",
		"`--json` is never capped",
	} {
		if !strings.Contains(c, sub) {
			t.Errorf("guide missing %q", sub)
		}
	}
}
