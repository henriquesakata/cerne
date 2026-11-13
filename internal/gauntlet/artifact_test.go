package gauntlet

import (
	"errors"
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
			name:   "criteria section with ### subheadings",
			in:     Input{Lang: doclang.PT, Spec: "## Objetivo e critérios de aceitação\n\n1. a\n\n### Detalhe\n\nmais\n\n## Verificação\n\nnao\n", Diff: "d"},
			has:    []string{"1. a", "### Detalhe", "mais"},
			hasNot: []string{"nao"},
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
			got, err := Assemble(tc.in)
			if err != nil {
				t.Fatal(err)
			}
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

func TestAssembleNoCriteria(t *testing.T) {
	for _, spec := range []string{"", "## Problema\n\nx\n"} {
		if _, err := Assemble(Input{Lang: doclang.PT, Spec: spec}); !errors.Is(err, ErrNoCriteria) {
			t.Errorf("spec %q: err = %v, want ErrNoCriteria", spec, err)
		}
	}
}

// planContracts reads the constraints section by position in the plan template.
func TestConstraintsIndexPinned(t *testing.T) {
	want := map[doclang.Lang]string{doclang.PT: "Restrições para o agente", doclang.EN: "Constraints for the agent"}
	for l, h := range want {
		if got := doclang.Sections(doclang.Plan, l)[constraintsIdx]; got != h {
			t.Errorf("%s: index %d = %q, want %q", l, constraintsIdx, got, h)
		}
	}
}

func TestAssembleEvidence(t *testing.T) {
	ev := []Evidence{{Name: "a.txt", Body: "out A\nline2\n"}, {Name: "b.log", Body: "out B"}}
	cases := []struct {
		lang    doclang.Lang
		ordered []string
	}{
		{doclang.PT, []string{"=== CRITÉRIOS (fim) ===", "=== EVIDÊNCIA: a.txt (início) ===", "out A\nline2", "=== EVIDÊNCIA: a.txt (fim) ===",
			"=== EVIDÊNCIA: b.log (início) ===", "out B", "=== EVIDÊNCIA: b.log (fim) ===", "=== DIFF (início) ==="}},
		{doclang.EN, []string{"=== CRITERIA (end) ===", "=== EVIDENCE: a.txt (start) ===", "out A\nline2", "=== EVIDENCE: a.txt (end) ===",
			"=== EVIDENCE: b.log (start) ===", "out B", "=== EVIDENCE: b.log (end) ===", "=== DIFF (start) ==="}},
	}
	for _, tc := range cases {
		got, err := Assemble(Input{Lang: tc.lang, Spec: specPT, Diff: "d", Evidence: ev})
		if err != nil {
			t.Fatal(err)
		}
		pos := 0
		for _, s := range tc.ordered {
			i := strings.Index(got[pos:], s)
			if i < 0 {
				t.Fatalf("%s: %q out of order/missing in:\n%s", tc.lang, s, got)
			}
			pos += i + len(s)
		}
	}
}

// criterion 7 by bytes: without evidence the bundle is what origin/main's
// Assemble produced for the same inputs (expected strings captured from it).
func TestAssembleGoldenWithoutEvidence(t *testing.T) {
	spec := "## Problema\n\nprosa\n\n## Objetivo e critérios de aceitação\n\n1. QUANDO x O SISTEMA DEVE y.\n\n## Verificação\n\nt\n"
	plan := "## Estratégia\n\nlivre\n\n## Contratos fixados pelo TL\n\n- contrato A\n"
	cases := []struct {
		in   Input
		want string
	}{
		{Input{Lang: doclang.PT, Spec: spec, Plan: plan, Notes: "desvio 1", Diff: "diff --git a/x b/x\n+a\n\n"},
			"=== CRITÉRIOS (início) ===\n## Objetivo e critérios de aceitação\n\n1. QUANDO x O SISTEMA DEVE y.\n\n## Contratos fixados pelo TL\n\n- contrato A\n\n## Desvios declarados pelo builder\n\ndesvio 1\n=== CRITÉRIOS (fim) ===\n\n=== DIFF (início) ===\ndiff --git a/x b/x\n+a\n=== DIFF (fim) ===\n"},
		{Input{Lang: doclang.EN, Spec: "## Goal and acceptance criteria\n\n1. WHEN a THE SYSTEM SHALL b.\n", Diff: "d\n"},
			"=== CRITERIA (start) ===\n## Goal and acceptance criteria\n\n1. WHEN a THE SYSTEM SHALL b.\n\n(no plan contracts)\n=== CRITERIA (end) ===\n\n=== DIFF (start) ===\nd\n=== DIFF (end) ===\n"},
	}
	for _, tc := range cases {
		got, err := Assemble(tc.in)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("%s: bytes differ:\n got %q\nwant %q", tc.in.Lang, got, tc.want)
		}
	}
}

// the evidence body is kept whole: only a missing final newline is added.
func TestAssembleEvidenceKeptWhole(t *testing.T) {
	got, err := Assemble(Input{Lang: doclang.EN, Spec: specPT, Diff: "d",
		Evidence: []Evidence{{Name: "a", Body: "x\n\n\n"}, {Name: "b", Body: "y"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"=== EVIDENCE: a (start) ===\nx\n\n\n=== EVIDENCE: a (end) ===", "=== EVIDENCE: b (start) ===\ny\n=== EVIDENCE: b (end) ==="} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
