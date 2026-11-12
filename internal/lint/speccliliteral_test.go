package lint

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// specWithCriteria wraps criteria lines in a spec whose "## Objetivo e
// critérios de aceitação" heading sits on file line 7 (frontmatter counts).
func specWithCriteria(criteria string) string {
	return "---\ncerne_id: F-0001\n---\n\n# T\n\n## Objetivo e critérios de aceitação\n\n" + criteria
}

func cliLiteralDetails(spec string) []string {
	var out []string
	for _, f := range SpecCLILiteralPT("F-0001", []byte(spec)) {
		out = append(out, f.Detail)
	}
	return out
}

// TestSpecCLILiteralPT (F-0353 criteria 1, 2, 4): a numbered criterion that
// quotes, between backticks, a CLI output literal the shared detector reads
// as pt-BR is flagged with the file line; the literal is the one that starts
// with "cerne:" or follows an output term in the same sentence; commands,
// paths and identifiers are never flagged.
func TestSpecCLILiteralPT(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want []string // exact details; nil = clean
	}{
		{"output term then pt-BR literal (crit. 1, 2)",
			specWithCriteria("1. QUANDO nada existe, O SISTEMA DEVE imprimir `nenhuma feature encontrada`.\n"),
			[]string{`line 9: CLI output literal reads pt-BR (pt-BR word "nenhuma"): nenhuma feature encontrada`}},
		{"every output term counts",
			specWithCriteria("1. O comando imprime `sem resultado`.\n2. A mensagem `sem resultado`.\n3. Recusa com `sem resultado`.\n4. No stderr `sem resultado`.\n5. No stdout `sem resultado`.\n6. A saída `sem resultado`.\n7. Um erro `sem resultado`.\n8. Pode imprimir `sem resultado`.\n"),
			[]string{
				`line 9: CLI output literal reads pt-BR (pt-BR word "sem"): sem resultado`,
				`line 10: CLI output literal reads pt-BR (pt-BR word "sem"): sem resultado`,
				`line 11: CLI output literal reads pt-BR (pt-BR word "sem"): sem resultado`,
				`line 12: CLI output literal reads pt-BR (pt-BR word "sem"): sem resultado`,
				`line 13: CLI output literal reads pt-BR (pt-BR word "sem"): sem resultado`,
				`line 14: CLI output literal reads pt-BR (pt-BR word "sem"): sem resultado`,
				`line 15: CLI output literal reads pt-BR (pt-BR word "sem"): sem resultado`,
				`line 16: CLI output literal reads pt-BR (pt-BR word "sem"): sem resultado`,
			}},
		{"cerne: prefix needs no term",
			specWithCriteria("1. Falha. Fica `cerne: clone de cache ausente` na tela.\n"),
			[]string{`line 9: CLI output literal reads pt-BR (pt-BR word "de"): cerne: clone de cache ausente`}},
		{"diacritic reason",
			specWithCriteria("1. Imprime `prioridade inválida`.\n"),
			[]string{"line 9: CLI output literal reads pt-BR (diacritic 'á'): prioridade inválida"}},
		{"English literal is clean",
			specWithCriteria("1. Imprime `no feature found`.\n"), nil},
		{"literal in another sentence than the term is not output",
			specWithCriteria("1. A mensagem é curta. Cita-se `nenhuma feature` só como exemplo.\n"), nil},
		{"literal with no term and no cerne: prefix is not output",
			specWithCriteria("1. O trecho `Barra: não aplicável` do documento fica como está.\n"), nil},
		{"a term inside an earlier backtick span is not a term",
			specWithCriteria("1. Termos: `imprime`, `erro`, `saída`.\n"), nil},
		{"command, path and identifier are never flagged (crit. 4)",
			specWithCriteria("1. Imprime `cerne feature approve F-1`.\n2. Imprime `internal/cli/x.go`.\n3. Imprime `err_não_ok`.\n4. Mensagem `spec-cli-literal-pt`.\n"), nil},
		{"outside the criteria section is ignored",
			specWithCriteria("1. Ok.\n\n## Desenho\n\n- Imprime `nenhuma feature encontrada`.\n"), nil},
		{"before the first numbered item is ignored",
			specWithCriteria("Imprime `nenhuma feature encontrada` aqui.\n\n1. Ok.\n"), nil},
		{"html comment guidance is ignored",
			specWithCriteria("<!-- imprime `nenhuma feature encontrada`\nmensagem `nenhuma coisa` -->\n1. Ok.\n"), nil},
		{"A1: a distant term is not 'right before' the literal",
			specWithCriteria("1. QUANDO o lint acha erro, O SISTEMA DEVE manter a linha `Barra: não aplicável` no documento.\n"), nil},
		{"A1: a listed bridge word still counts",
			specWithCriteria("1. O sistema imprime no stderr `sem resultado`.\n2. Recusa com a mensagem `sem resultado`.\n"),
			[]string{
				`line 9: CLI output literal reads pt-BR (pt-BR word "sem"): sem resultado`,
				`line 10: CLI output literal reads pt-BR (pt-BR word "sem"): sem resultado`,
			}},
		{"A2: the Gauntlet block after the list is not scanned",
			specWithCriteria("1. Ok.\n\nBarra: ref nomeada\nMétodo: comparação cega\nSaída: vencer ou `sem resultado`\n"), nil},
		{"A2: a non-indented line right after the item ends it",
			specWithCriteria("1. Ok.\nSaída: `sem resultado`\n2. Imprime `sem resultado`.\n"),
			[]string{`line 11: CLI output literal reads pt-BR (pt-BR word "sem"): sem resultado`}},
		{"continuation line of a criterion keeps its file line",
			specWithCriteria("1. QUANDO falta, O SISTEMA DEVE\n   imprimir `nenhuma feature encontrada`.\n"),
			[]string{`line 10: CLI output literal reads pt-BR (pt-BR word "nenhuma"): nenhuma feature encontrada`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cliLiteralDetails(tc.spec)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("details:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

// TestSpecCLILiteralFindings: the rule reads candidate and approved specs
// from the backend and is advisory (F-0353 criteria 1, 6).
func TestSpecCLILiteralFindings(t *testing.T) {
	if !Advisory(RuleSpecCLILiteralPT) {
		t.Fatal("spec-cli-literal-pt must be advisory")
	}
	if _, ok := ruleRank[RuleSpecCLILiteralPT]; !ok {
		t.Fatal("RuleSpecCLILiteralPT missing from ruleRank")
	}
	spec := specWithCriteria("1. Imprime `nenhuma feature encontrada`.\n")
	for state, want := range map[domain.FeatureState]int{
		domain.StateCandidate: 1, domain.StateApproved: 1, domain.StateInDevelopment: 0, domain.StateImplemented: 0,
	} {
		dir := t.TempDir()
		if err := store.SaveFeature(dir, store.Feature{ID: "F-0001", Title: "t", State: state}); err != nil {
			t.Fatal(err)
		}
		writeDoc(t, dir, "content/features/F-0001/spec.md", spec)
		got, err := SpecCLILiteralFindings(dir)
		if err != nil {
			t.Fatalf("%s: %v", state, err)
		}
		if len(got) != want {
			t.Fatalf("%s: findings = %v, want %d", state, got, want)
		}
		if want == 1 && (got[0].Rule != RuleSpecCLILiteralPT || got[0].Entity != "F-0001" || got[0].File != "content/features/F-0001/spec.md") {
			t.Errorf("%s: finding = %+v", state, got[0])
		}
	}
}
