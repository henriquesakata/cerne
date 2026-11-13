package gauntlet

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
)

const specPT = "# T\n\n## Problema\n\nprosa\n\n## Objetivo e critérios de aceitação\n\n1. QUANDO x O SISTEMA DEVE y.\n\n```\n## nao e secao\n```\n\n## Verificação\n\ntestes\n"
const planPT = "# P\n\n## Estratégia\n\nlivre\n\n## Contratos fixados pelo TL\n\n- contrato A\n\n## Restrições para o agente\n\n- nada de X\n\n## Definição de pronto\n\n- [ ] ok\n"

func TestAssemble(t *testing.T) {
	cases := []struct {
		name    string
		in      Input
		has     []string
		hasNot  []string
		ordered []string
	}{
		{
			name: "pt with contracts and notes",
			in:   Input{Lang: doclang.PT, Spec: specPT, Plan: planPT, Notes: "desvio 1", Diff: "diff --git a/x b/x\n"},
			has: []string{"## Objetivo e critérios de aceitação", "QUANDO x O SISTEMA DEVE y", "## nao e secao",
				"## Contratos fixados pelo TL", "- contrato A", "## Restrições para o agente",
				"## Desvios declarados pelo builder", "desvio 1", "diff --git a/x b/x"},
			hasNot: []string{"prosa", "livre", "## Verificação", "## Estratégia", "Definição de pronto", "(no plan contracts)"},
			ordered: []string{"=== CRITÉRIOS (início) ===", "## Objetivo", "## Contratos", "## Desvios", "=== CRITÉRIOS (fim) ===",
				"=== DIFF (início) ===", "diff --git", "=== DIFF (fim) ==="},
		},
		{
			name: "en without plan or notes",
			in: Input{Lang: doclang.EN, Spec: "## Problem\n\nx\n\n## Goal and acceptance criteria\n\n1. WHEN a THE SYSTEM SHALL b.\n",
				Plan: "", Diff: "d\n"},
			has:    []string{"=== CRITERIA (start) ===", "=== CRITERIA (end) ===", "=== DIFF (start) ===", "=== DIFF (end) ===", "(no plan contracts)", "WHEN a THE SYSTEM SHALL b"},
			hasNot: []string{"Builder's declared deviations"},
		},
		{
			name:   "plan with only template sections",
			in:     Input{Lang: doclang.PT, Spec: specPT, Plan: "## Estratégia\n\nx\n\n## Definição de pronto\n\ny\n", Diff: ""},
			has:    []string{"(no plan contracts)"},
			hasNot: []string{"## Estratégia"},
		},
		{
			name: "en notes title and EN template sections",
			in: Input{Lang: doclang.EN, Spec: "## Goal and acceptance criteria\n\n1. a\n",
				Plan: "## Strategy\n\ns\n\n## Constraints for the agent\n\n- c\n\n## Owner adjustments\n\n- o\n", Notes: "n", Diff: "d"},
			has:    []string{"## Builder's declared deviations", "## Constraints for the agent", "## Owner adjustments"},
			hasNot: []string{"## Strategy"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Assemble(tc.in)
			for _, s := range tc.has {
				if !strings.Contains(got, s) {
					t.Errorf("missing %q in:\n%s", s, got)
				}
			}
			for _, s := range tc.hasNot {
				if strings.Contains(got, s) {
					t.Errorf("unexpected %q in:\n%s", s, got)
				}
			}
			pos := 0
			for _, s := range tc.ordered {
				i := strings.Index(got[pos:], s)
				if i < 0 {
					t.Fatalf("%q out of order/missing", s)
				}
				pos += i + len(s)
			}
		})
	}
}
