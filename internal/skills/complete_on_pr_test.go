package skills

import (
	"strings"
	"testing"
)

// B-0027: human review starts on Ready, so the skill every agent loads says
// to complete as soon as the PR is open, never after CI or the merge.
func TestSkillCompletesAsSoonAsThePRIsOpen(t *testing.T) {
	want := "Run `cerne feature complete` as soon as the PR is open"
	if !strings.Contains(All()[0].Content, want) {
		t.Fatalf("SKILL.md lacks %q", want)
	}
}
