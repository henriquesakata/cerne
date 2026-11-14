package hooks

import (
	"strings"
	"testing"
)

// firstBefore fails unless s carries a and b, a first.
func firstBefore(t *testing.T, name, s, a, b string) {
	t.Helper()
	i, j := strings.Index(s, a), strings.Index(s, b)
	if i < 0 || j < 0 || i > j {
		t.Errorf("%s: want %q before %q:\n%s", name, a, b, s)
	}
}

// TestStaticGuidance_Bilingual (F-0418 crit. 6): the session rules are
// installed without knowing the project, so they name the English form
// first and the Portuguese one as its equivalent — the copy embedded in the
// installed script included.
func TestStaticGuidance_Bilingual(t *testing.T) {
	for name, s := range map[string]string{
		"GauntletRule":     GauntletRule,
		"RegisterRules":    RegisterRules,
		"SessionRules(on)": SessionRules(true),
		"installed script": RegisterRules, // providers.go embeds this very constant
	} {
		firstBefore(t, name, s, "`Bar:`", "`Barra:`")
		firstBefore(t, name, s, "Verification", "Verificação")
	}
}
