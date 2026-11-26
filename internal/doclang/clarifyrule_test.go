package doclang

import (
	"strings"
	"testing"
)

// The one rule text of F-0517: the same sentence in the session rule, the
// skill's reference and every drafting prompt (each prompt ends with
// Directive, which the prompt tests pin as its suffix).
// F-0517 crit. 7, 8
func TestClarifyRuleInDirective(t *testing.T) {
	for _, l := range []Lang{PT, EN} {
		rule := ClarifyRule(l)
		if !strings.Contains(rule, ClarifyForm(l)) {
			t.Errorf("%s: the rule lacks the form with options and a recommended one: %s", l, rule)
		}
		if !strings.Contains(Directive(l), rule) {
			t.Errorf("%s: Directive lacks the clarification rule", l)
		}
	}
	if !strings.Contains(ClarifyRule(PT), "(recomendada)") || !strings.Contains(ClarifyRule(EN), "(recommended)") {
		t.Error("the example is not in the project's docs language")
	}
}
