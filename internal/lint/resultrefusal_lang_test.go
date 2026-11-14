package lint

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// ptProse are words of the Portuguese prose the result refusals carried
// before F-0418: none may survive, in any project (N-0068).
var ptProse = []string{"sem evidência", "sem seção", "sem linha", "sem citação", "sem veredito", "edite", "rode", "Forma", "prova", "congelado", "anexe", "citando", "dentro de"}

// TestResultRefusal_EnglishProse (F-0418 crit. 4 and 5): the result refusals
// — missing evidence, missing Gauntlet record — read English in every
// project and cite the literal forms of the project's documentation
// language, in every correction state.
func TestResultRefusal_EnglishProse(t *testing.T) {
	cases := []struct {
		lang, spec, result string
		forms, foreign     []string
	}{
		{"en", "## Goal and acceptance criteria\n\nGoal.\n\n1. a\n2. b\n", "criterion 1 proved.\n",
			[]string{`cite "criterion N"`, `"Bar:"`, `"Round N — <win|loss|tie>: \"<judge's quote>\""`, `"Critic findings:"`, `"Bar: not applicable (<justification>)"`, `no "## Gauntlet" section`},
			[]string{"critério", "Barra", "Rodada", "vitória", "Achados", "não aplicável"}},
		{"pt-BR", checkSpec, "critério 1 provado.\n",
			[]string{`cite "critério N"`, `"Barra:"`, `"Rodada N — <vitória|derrota|empate>: \"<judge's quote>\""`, `"Achados do crítico:"`, `"Barra: não aplicável (<justification>)"`, `no "## Gauntlet" section`},
			[]string{"criterion N", `"Bar:"`, "Round N", "Critic findings"}},
	}
	for _, tc := range cases {
		for _, state := range []domain.FeatureState{domain.StateInDevelopment, domain.StateImplemented, domain.StateDone} {
			dir, f := writeResultFixture(t, state, tc.spec, tc.result)
			if err := store.SaveProject(dir, store.Project{DocsLanguage: tc.lang}); err != nil {
				t.Fatal(err)
			}
			got := CheckResult(dir, f)
			if len(got) != 2 {
				t.Fatalf("%s %s: want evidence + gauntlet findings, got %+v", tc.lang, state, got)
			}
			var details string
			for _, fd := range got {
				details += fd.Detail + "\n"
			}
			for _, w := range tc.forms {
				if !strings.Contains(details, w) {
					t.Errorf("%s %s: details lack %q:\n%s", tc.lang, state, w, details)
				}
			}
			for _, w := range append(append([]string{}, ptProse...), tc.foreign...) {
				if strings.Contains(details, w) {
					t.Errorf("%s %s: details carry %q:\n%s", tc.lang, state, w, details)
				}
			}
		}
	}
}
