package hooks

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// F-0517 crit. 7
func TestSessionRulesTeachClarifyOptions(t *testing.T) {
	for _, l := range []doclang.Lang{doclang.PT, doclang.EN} {
		for _, got := range []string{SessionRules(true, l), SessionRules(false, l), LangRules(l)} {
			if !strings.Contains(got, doclang.ClarifyRule(l)) {
				t.Errorf("%s: the session rule lacks the clarification rule:\n%s", l, got)
			}
		}
	}
}
