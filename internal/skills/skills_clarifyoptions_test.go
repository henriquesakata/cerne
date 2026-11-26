package skills

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// F-0517 crit. 7: the authoring skill carries the clarification rule — the
// options form in SKILL.md, the full rule text (the same sentence the session
// rule and the drafting prompts carry, in both languages) in the reference.
func TestCerneAuthoringTeachesClarifyOptions(t *testing.T) {
	c := fullAuthoring(t)
	for _, want := range []string{"option (recommended)", "opção (recomendada)", doclang.ClarifyRule(doclang.PT), doclang.ClarifyRule(doclang.EN)} {
		if !strings.Contains(c, want) {
			t.Errorf("skill missing %q", want)
		}
	}
}
