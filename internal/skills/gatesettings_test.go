package skills

import (
	"strings"
	"testing"
)

// TestHumanGatesNameTheGateSettings: the Human gates section every session
// loads names both gate settings, says they are the owner's and that the
// agent never changes them to cross a gate.
// F-0515 crit. 11
func TestHumanGatesNameTheGateSettings(t *testing.T) {
	c := All()[0].Content
	_, section, ok := strings.Cut(c, "### Human gates")
	if !ok {
		t.Fatal("SKILL.md has no Human gates section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	for _, want := range []string{"`gates.approve`", "`gates.accept`", "owner's", "never change them to cross a gate"} {
		if !strings.Contains(section, want) {
			t.Errorf("Human gates section lacks %q", want)
		}
	}
}
