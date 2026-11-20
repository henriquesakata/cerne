package skills

import (
	"os"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/hooks"
)

// E-0005 criterion 4 (both halves, T-0014 and T-0017): the guide and the
// session start send an agent to `cerne next` first and to the package of its
// task, `cerne context <id>`, not to reading CONTEXT.md, which is the map of
// what is live and of the tags; and
// (criterion 21) the guide has an existing component picked before a new one.
func TestGuideStartsFromNextAndPicksAnExistingComponent(t *testing.T) {
	b, err := os.ReadFile("cerne-authoring/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	skill := string(b)
	for _, want := range []string{
		"Start a session with `cerne next`",
		"then a task with its package, `cerne context <id>`",
		"`.docs/CONTEXT.md` is the map of what is live",
		"pick an existing component (`cerne tags`) before creating one",
	} {
		if !strings.Contains(skill, want) {
			t.Errorf("SKILL.md missing %q", want)
		}
	}
	if strings.Contains(skill, "Read current state from `.docs/CONTEXT.md`") || strings.Contains(skill, "Read current state from .docs/CONTEXT.md") {
		t.Error("SKILL.md still sends the agent to read CONTEXT.md first")
	}
	for name, rules := range map[string]string{
		"static":  hooks.RegisterRules,
		"session": hooks.SessionRules(true, doclang.PT),
	} {
		if !strings.Contains(rules, "START a session with `cerne next`") || !strings.Contains(rules, "`cerne tags`") ||
			!strings.Contains(rules, "then a task with its package, `cerne context <id>`") {
			t.Errorf("%s session-start rules do not start from cerne next and cerne tags:\n%s", name, rules)
		}
	}
}
