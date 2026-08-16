package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsGauntlet pins F-0274 (crit. 1): the canonical
// authoring skill teaches the Gauntlet protocol across its three phases — the
// bar in the spec (F-0270), the adversarial loop during implementation, and
// the per-criterion evidence the result owes (what F-0272's lint charges).
// Only contractual terms and the literal Verificação forms are pinned; the
// surrounding prose is free to be rewritten.
func TestCerneAuthoringDocumentsGauntlet(t *testing.T) {
	c := All()[0].Content

	contains := func(crit, sub string) {
		t.Helper()
		if !strings.Contains(c, sub) {
			t.Errorf("%s: skill missing %q", crit, sub)
		}
	}

	// The section exists and is findable by name.
	contains("crit1", "Gauntlet")

	// (a) Spec phase: a named + reachable bar of the same class, and the three
	// literal forms F-0270 writes into the Verificação section — verbatim
	// pt-BR, since they are the text an agent writes inside a pt-BR spec.
	contains("crit1a", "Método: comparação cega")
	contains("crit1a", "Saída: vencer ou empatar")
	contains("crit1a", "Barra: não aplicável")
	contains("crit1a", "Barra: dispensada por decisão humana")
	contains("crit1a", "named")
	contains("crit1a", "same class")

	// (b) Implementation phase: TDD builders, a fresh-context critic over the
	// diff, a blind judge against the bar, and the exit condition — win or
	// tie, or a recorded human decision to stop; never a round counter.
	contains("crit1b", "builders")
	contains("crit1b", "fresh-context critic")
	contains("crit1b", "blind judge")
	contains("crit1b", "win or tie")
	contains("crit1b", "never a fixed number of rounds")

	// (c) Closing phase: result.md cites the criterion number by name, which
	// is exactly what the result-missing-evidence lint rule charges.
	contains("crit1c", "critério N")
	contains("crit1c", "result-missing-evidence")

	// F-0276: bar and artifact are extracted in the form the consumer sees
	// (rendered runtime value, served page, command output), never cut from
	// the source — the assembly layer resolves away in the product — and a
	// form finding is verified against the runtime before it becomes a
	// correction, a debt or a feature.
	contains("F-0276/1", "the rendered runtime value")
	contains("F-0276/1", "never as a cut from the source")
	contains("F-0276/1", "resolves away in the product")
	contains("F-0276/2", "by the same consumer path")
	contains("F-0276/2", "assembly residue")
	contains("F-0276/2", "verify it against the runtime")
}

// TestCerneAuthoringDocumentsGauntletFieldRules pins F-0274 (crit. 2): the
// rules that real rounds validated in F-0270/F-0271/F-0272 — a critic is
// never the builder, the judged artifact is explicitly delimited in the
// critic's prompt (the W-0181 lesson: instructions leaking into the artifact),
// a new judge each round, and a defeat produces corrections and another round.
func TestCerneAuthoringDocumentsGauntletFieldRules(t *testing.T) {
	c := All()[0].Content

	contains := func(crit, sub string) {
		t.Helper()
		if !strings.Contains(c, sub) {
			t.Errorf("%s: skill missing %q", crit, sub)
		}
	}

	contains("crit2", "never the builder")
	contains("crit2", "delimit")
	contains("crit2", "a new judge every round")
	contains("crit2", "defeat produces corrections")
}
