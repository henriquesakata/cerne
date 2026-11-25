package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsIdeas pins F-0312 crit. 13: the canonical
// authoring skill exposes the idea verbs, the rule "an idea is not a demand"
// and the on-demand policy for agents (crit. 9).
func TestCerneAuthoringDocumentsIdeas(t *testing.T) {
	c := fullAuthoring(t)
	for _, sub := range []string{
		"cerne idea new",
		"cerne idea list",
		"cerne idea show",
		"cerne show I-NNNN",
		"cerne idea drop",
		"cerne idea promote",
		"not a demand",
		"--search",
		"On demand only (agents)", // not "on demand": that one is on main already (C17)
		"never pull ideas in unasked",
	} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}

// F-0510 crit. 9: the promote bullet teaches classifying the card in the same
// act — the tag and capability flags, and the refusal's way out.
func TestCerneAuthoringPromoteClassifiesInTheSameAct(t *testing.T) {
	var bullet string
	for _, line := range strings.Split(fullAuthoring(t), "\n") {
		if strings.HasPrefix(line, "- **Promote / link:**") {
			bullet = line
		}
	}
	for _, sub := range []string{"--facet", "--component", "--capability", "--allow-incomplete", "same act"} {
		if !strings.Contains(bullet, sub) {
			t.Errorf("promote bullet lacks %q:\n%s", sub, bullet)
		}
	}
}
