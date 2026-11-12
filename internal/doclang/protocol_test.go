package doclang

import (
	"reflect"
	"testing"
)

func TestBarLine(t *testing.T) {
	tests := []struct {
		line, value string
		lang        Lang
		ok          bool
	}{
		{"Barra: internal/x.go", "internal/x.go", PT, true},
		{"- **barra (da spec):** x", "x", PT, true},
		{"Barra lateral: x", "x", PT, true}, // the loose layout, as it always matched
		{"Bar: internal/x.go", "internal/x.go", EN, true},
		{"> **Bar (from the spec):** x", "x", EN, true},
		{"bar: 42", "", "", false},
		{"Bar chart: totals", "", "", false},
		{"Barcelona: office", "", "", false},
		{"no label here", "", "", false},
	}
	for _, tt := range tests {
		v, l, ok := BarLine(tt.line)
		if v != tt.value || l != tt.lang || ok != tt.ok {
			t.Errorf("BarLine(%q) = %q, %q, %v; want %q, %q, %v", tt.line, v, l, ok, tt.value, tt.lang, tt.ok)
		}
	}
}

func TestExemption(t *testing.T) {
	tests := []struct {
		value    string
		term     Term
		lang     Lang
		inner    string
		complete bool
		ok       bool
	}{
		{"não aplicável (correção pontual) — padrão alternativo: testes", NotApplicable, PT, "correção pontual", true, true},
		{"nao-aplicavel (x)", NotApplicable, PT, "x", true, true},
		{"not applicable (sample data)", NotApplicable, EN, "sample data", true, true},
		{"Not applicable", NotApplicable, EN, "", false, true},
		{`dispensada por decisão humana ("pode pular")`, Waived, PT, `"pode pular"`, true, true},
		{"dispensada", Waived, PT, "", false, true},
		{`waived by human decision ("ship it")`, Waived, EN, `"ship it"`, true, true},
		{"waived by the team", Waived, EN, "", false, true},
		{"Waived-fee checkout flow", 0, "", "", false, false}, // a named reference
		{"notable prior art", 0, "", "", false, false},
		{"internal/x.go", 0, "", "", false, false},
	}
	for _, tt := range tests {
		term, lang, inner, complete, ok := Exemption(tt.value)
		if ok != tt.ok || (ok && (term != tt.term || lang != tt.lang || inner != tt.inner || complete != tt.complete)) {
			t.Errorf("Exemption(%q) = %d, %q, %q, %v, %v", tt.value, term, lang, inner, complete, ok)
		}
	}
}

func TestRoundAnchor(t *testing.T) {
	for line, want := range map[string]int{
		"Rodada 1 — vitória": 1, "Rodada 6 de novo": 6, // loose
		"Round 2 — win": 2, "Round 3": 3, "Round 4: tie": 4, "Round 5 (new judge) — loss": 5,
	} {
		if n, ok := RoundAnchor(line); !ok || n != want {
			t.Errorf("RoundAnchor(%q) = %d, %v; want %d", line, n, ok, want)
		}
	}
	for _, line := range []string{"Round 2 of benchmarking was skipped", "Round 10 times and average", "Rounding 2 digits", "rodada 1", "Rodadas 2"} {
		if _, ok := RoundAnchor(line); ok {
			t.Errorf("RoundAnchor(%q) opened a round", line)
		}
	}
}

func TestOutcome(t *testing.T) {
	for s, want := range map[string]string{
		"Rodada 2 (tie-break) — derrota":   "derrota",
		"Round 2 — tie-breaker run — loss": "loss",
		" — WIN: ":                         "WIN",
		"uma vitória e uma derrota":        "vitória",
		"a win-win, they said":             "",
		"lossless and untied":              "",
	} {
		if got := Outcome(s); got != want {
			t.Errorf("Outcome(%q) = %q, want %q", s, got, want)
		}
	}
}

func TestOutcomeLine(t *testing.T) {
	for s, want := range map[string]string{
		"Veredito: derrota": "derrota", "Derrota técnica": "Derrota", "Resultado: EMPATE.": "EMPATE",
		"Loss.": "Loss", "Verdict: tie": "tie", "Outcome: **win**": "win", "win — after the fix": "win",
		"Loss of precision was noted": "", "Win rate: 60%": "", "Tie the output to the fixture": "", "Win for A on clarity.": "",
	} {
		if got := OutcomeLine(s); got != want {
			t.Errorf("OutcomeLine(%q) = %q, want %q", s, got, want)
		}
	}
}

func TestVerdictIn(t *testing.T) {
	for _, text := range []string{"VEREDITO: EMPATE", "o juiz deu vitória ao A", "VERDICT: A", "ended with \"VERDICT: B\".", "line one\nVerdict: TIE\nline three"} {
		if !VerdictIn(text) {
			t.Errorf("VerdictIn(%q) = false", text)
		}
	}
	for _, text := range []string{"Verdict: A clear improvement", "we tie the outputs; a quick win", "nothing here"} {
		if VerdictIn(text) {
			t.Errorf("VerdictIn(%q) = true", text)
		}
	}
	if LooseOutcome("a clear win") || !LooseOutcome("uma derrota clara") {
		t.Error("LooseOutcome must read the loose languages only")
	}
}

func TestHeaderAndCriticLines(t *testing.T) {
	for _, s := range []string{"Método: comparação cega", "saída: vencer", "Method: blind comparison", "Exit: win or tie"} {
		if !HeaderLine(s) {
			t.Errorf("HeaderLine(%q) = false", s)
		}
	}
	for _, s := range []string{"exit: 0", "method: POST", "Methodology: none"} {
		if HeaderLine(s) {
			t.Errorf("HeaderLine(%q) = true", s)
		}
	}
	for _, s := range []string{"Achados do crítico: 3", "Critic findings: 2", "critic’s finding: 1"} {
		if !CriticLine(s) {
			t.Errorf("CriticLine(%q) = false", s)
		}
	}
	if CriticLine("Critic findingsX") {
		t.Error("CriticLine matched inside a longer word")
	}
}

func TestLegacyExemption(t *testing.T) {
	if !LegacyExemption("texto. Barra: não aplicável (correção pontual)") || !LegacyExemption("Barra: dispensada por decisão humana (\"ok\")") {
		t.Error("the Portuguese legacy forms must still vouch")
	}
	if LegacyExemption("Bar: not applicable (x)") {
		t.Error("no English record predates the strict reader")
	}
}

func TestCitations(t *testing.T) {
	tests := []struct {
		text string
		want []Citation
	}{
		{"critério 2", []Citation{{Numbers: []int{2}}}},
		{"critérios 1-3 e 5", []Citation{{Plural: true, Numbers: []int{1, 3, 5}, Ranges: [][2]int{{1, 3}}}}},
		{"critérios 2 a 4", []Citation{{Plural: true, Numbers: []int{2, 4}, Ranges: [][2]int{{2, 4}}}}},
		{"Criterion 2 to 3 hours", []Citation{{Numbers: []int{2, 3}, Ranges: [][2]int{{2, 3}}}}}, // singular: the reader takes the first number only
		{"criteria 1 to 3, 5 and 7", []Citation{{Plural: true, Numbers: []int{1, 3, 5, 7}, Ranges: [][2]int{{1, 3}}}}},
		{"critérios 1 to 3", []Citation{{Plural: true, Numbers: []int{1}}}}, // each language joins with its own words
		{"subcriterion 3", nil},
		{"the criterion was met; see 1", nil},
	}
	for _, tt := range tests {
		if got := Citations(tt.text); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Citations(%q) = %+v, want %+v", tt.text, got, tt.want)
		}
	}
}
