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
	"```bash\n# S99 num comentario nao e passo\nexit 1\n```\n\nNa janela S10 repete. S3A nao e passo.\n"

// F-0873: steps come from tokens in the body, not from headings.
func TestRunbookStepsRangeAndFences(t *testing.T) {
	got := RunbookSteps(bloco5aBody)
	want := []string{"S2", "S3", "S4", "S5", "S5.1", "S6", "S6.6", "S7", "S9", "S10"}
	if !slices.Equal(got, want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
}
