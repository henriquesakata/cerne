package cli

import (
	"os"
	"regexp"
	"testing"
)

// B-0056: the number measures the reduction of the corpus the agent searches,
// not context savings, tokens saved or content kept out of the agent's context.
var savingsOldTerm = regexp.MustCompile(`(?i)context savings|economia de contexto|out of the agent.s context|fora do contexto do agente|tokens saved|economizad|poupa|keeps out of reach|what the projection keeps out|stats: '(Savings|Economia)'`)

func TestSavingsWordingHasNoOldTerm(t *testing.T) {
	cmd := newSavingsCmd()
	texts := map[string]string{
		"short": cmd.Short,
		"long":  cmd.Long,
	}
	for _, f := range []string{"../../ui/src/i18n.tsx", "../../README.md"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		texts[f] = string(b)
	}
	for name, txt := range texts {
		if m := savingsOldTerm.FindString(txt); m != "" {
			t.Errorf("%s still uses the old term %q", name, m)
		}
	}
}
