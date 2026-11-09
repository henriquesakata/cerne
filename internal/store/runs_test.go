package store

import (
	"slices"
	"testing"
)

// bloco5aBody imitates the RB-0070 structure the TL measured: S3 to S7 live in
// one block heading, S6.6 has no heading, S2 and S9 are named in an unrelated
// title, and a bash comment names a step. It is a fixture: the real RB-0070 is
// never read.
const bloco5aBody = "# Rotacao\n\n## Releases de producao entre o passo S2 e o S9\n\nTexto.\n\n" +
	"### Bloco 5a: giro (passos S3 a S7)\n\n- S3 confere\n- S4 gira\n- S5.1 sonda\n- S6 aplica\n- S6.6 parada: se o alerta disparar\n- S7 fecha\n\n" +
	"- S4b sub-passo e S4g ultimo; S4b.1 detalhe; S3a tambem\n\n" +
	"```bash\n# S99 num comentario nao e passo\nexit 1\n```\n\nNa janela S10 repete. S3A (maiuscula) e S4bc nao sao passo.\n"

// F-0873: steps come from tokens in the body, not from headings.
func TestRunbookStepsRangeAndFences(t *testing.T) {
	got := RunbookSteps(bloco5aBody)
	want := []string{"S2", "S3", "S3a", "S4", "S4b", "S4b.1", "S4g", "S5", "S5.1", "S6", "S6.6", "S7", "S9", "S10"}
	if !slices.Equal(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
}

// Review finding: a token with a lowercase letter after the number (S3a, S4g)
// is a step, as in the real RB-0070; none may vanish silently. Order: the
// dotted children of a number come before its letter variants (S4 < S4.1 <
// S4a < S4a.1 < S4b < S5), each part compared as (number, letter).
func TestCompareStepsLetterSuffix(t *testing.T) {
	want := []string{"S3", "S3a", "S4", "S4.1", "S4a", "S4a.1", "S4b", "S4g", "S5", "S10"}
	got := slices.Clone(want)
	slices.Reverse(got)
	slices.SortFunc(got, CompareSteps)
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}
