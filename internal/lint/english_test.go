package lint

import (
	"reflect"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
)

func doc(l doclang.Lang, k doclang.Kind) []byte {
	var b strings.Builder
	for _, s := range doclang.Sections(k, l) {
		b.WriteString("## " + s + "\n\ntext\n\n")
	}
	return []byte(b.String())
}

// F-0359 criterion 4: a document with every section under its English heading
// is complete, whatever language the project scaffolds in.
func TestIncompleteDoc_EnglishSections(t *testing.T) {
	for _, k := range []doclang.Kind{doclang.Spec, doclang.Plan, doclang.Result, doclang.Decision, doclang.Runbook} {
		for _, project := range []doclang.Lang{doclang.PT, doclang.EN} {
			if missing := missingSections(doc(doclang.EN, k), k, project); missing != nil {
				t.Errorf("kind %d, project %s: English document missing %v", k, project, missing)
			}
			if missing := missingSections(doc(doclang.PT, k), k, project); missing != nil {
				t.Errorf("kind %d, project %s: Portuguese document missing %v", k, project, missing)
			}
		}
	}
}

// F-0359 criterion 5: headings may mix languages; a section absent in both is
// reported in the project's language.
func TestIncompleteDoc_MixedLanguages(t *testing.T) {
	mixed := []byte("## Contexto\n\n## Decision\n\n## Consequências\n\n## Alternatives considered\n\n## Implementation and relations\n")
	if missing := missingSections(mixed, doclang.Decision, doclang.PT); missing != nil {
		t.Errorf("mixed document missing %v", missing)
	}
	partial := []byte("## Context\n\n## Decisão\n")
	if got, want := missingSections(partial, doclang.Decision, doclang.EN), []string{"Consequences", "Alternatives considered", "Implementation and relations"}; !reflect.DeepEqual(got, want) {
		t.Errorf("missing = %v, want %v", got, want)
	}
	if got := missingSections(partial, doclang.Decision, doclang.PT); len(got) != 3 || got[0] != "Consequências" {
		t.Errorf("missing in pt = %v", got)
	}
}

// F-0359 criterion 7: numbered criteria under the English heading, cited in
// English in the result.
func TestResultEvidence_EnglishCriteria(t *testing.T) {
	spec := []byte("## Problem\n\n1. not a criterion\n\n## Goal and acceptance criteria\n\nOne goal.\n\n1. WHEN a THE SYSTEM SHALL b.\n2. WHEN c THE SYSTEM SHALL d.\n3. Third.\n\n## Design decisions\n\n4. not a criterion\n")
	if got, want := specCriteria(spec), []int{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Fatalf("specCriteria = %v, want %v", got, want)
	}
	tests := []struct {
		result string
		want   []int
	}{
		{"- Criterion 1: MET. TestA.\n- criterion 3: MET.", []int{1, 3}},
		{"Criteria 1-3 are covered by TestAll.", []int{1, 2, 3}},
		{"criteria 1 to 2 pass", []int{1, 2}},
		{"Criteria: 1, 3 and 2.", []int{1, 2, 3}},
		{"criterion 2 to 3 hours of work", []int{2}}, // singular never expands
		{"1. MET\n2. MET\n3. MET", nil},              // a bare number is not a citation
		{"The criterion was met; see 1 and 2.", nil}, // the number must follow the word
		{"critério 1 e criterion 2", []int{1, 2}},    // both vocabularies in one result
	}
	for _, tt := range tests {
		got := resultMentions([]byte(tt.result))
		for _, n := range []int{1, 2, 3} {
			wanted := false
			for _, w := range tt.want {
				wanted = wanted || w == n
			}
			if got[n] != wanted {
				t.Errorf("%q: criterion %d cited = %v, want %v", tt.result, n, got[n], wanted)
			}
		}
	}
}

// F-0359 criterion 8: the English Gauntlet record.
func TestResultGauntlet_English(t *testing.T) {
	tests := []struct {
		name    string
		section string
		problem string
	}{
		{"complete", "Bar: internal/x.go — `cat` it\nRound 1 — win: \"A is clearer on all four points\"\nCritic findings: 2 — fixed 2\n", ""},
		{"loss then win", "Bar: internal/x.go\nRound 1 — loss: \"B handles the empty case\"\nRound 2 — win: \"A now covers it\"\n", ""},
		{"exemption", "Bar: not applicable (sample data, nothing to compare against)\n", ""},
		{"waived", "Bar: waived by human decision (\"ship it\")\n", ""},
		{"verdict literal", "Bar: internal/x.go\nRound 1: the judge wrote \"VERDICT: TIE\" after comparing both\n", ""},
		{"round without a quote", "Bar: internal/x.go\nRound 1 — win\n", "rodada 1 sem citação do juiz"},
		{"round without a verdict", "Bar: internal/x.go\nRound 1: \"both read fine to me\"\n", "rodada 1 sem veredito"},
		{"no bar", "Round 1 — win: \"A is better\"\n", `sem linha "Barra:"`},
		{"critic-only pass is not a round", "Bar: internal/x.go\nCritic findings: 3 — fixed 3, one was \"a clear win\"\n", "rodada sem citação do juiz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseGauntlet(tt.section).problem(); got != tt.problem {
				t.Errorf("problem = %q, want %q", got, tt.problem)
			}
		})
	}
}

func TestGauntletRounds_English(t *testing.T) {
	result := []byte("## Gauntlet\n\nBar: internal/x.go\nRound 1 — loss: \"B wins on robustness\"\nRound 2 — tie: \"no difference left\"\n")
	if ok, losses := GauntletRounds(result); !ok || losses != 1 {
		t.Errorf("GauntletRounds = %v, %d; want true, 1", ok, losses)
	}
	if v, ok := GauntletVerdict(result); !ok || v != "tie" {
		t.Errorf("GauntletVerdict = %q, %v; want tie", v, ok)
	}
}

// Everyday English must not turn into protocol (critic, round 1): each line
// below was misread by the first, loose version of the English forms.
func TestResultGauntlet_EnglishIsStrict(t *testing.T) {
	t.Run("a compound does not flip the outcome", func(t *testing.T) {
		for _, section := range []string{
			"Bar: internal/x.go\nRound 2 — tie-breaker run — loss: \"B wins on robustness\"\n",
			"Barra: internal/x.go\nRodada 2 (tie-break) — derrota: \"B vence em robustez\"\n",
		} {
			if ok, losses := GauntletRounds([]byte("## Gauntlet\n\n" + section)); !ok || losses != 1 {
				t.Errorf("%q: GauntletRounds = %v, %d; want true, 1", section, ok, losses)
			}
		}
	})
	t.Run("prose does not state an outcome", func(t *testing.T) {
		for _, line := range []string{
			"Loss of precision was noted on input 3.",
			"Win rate: 60%",
			"Tie the output to the fixture",
			"Win for A on clarity.",
			"Result: winning is not the point",
		} {
			if w := doclang.OutcomeLine(line); w != "" {
				t.Errorf("OutcomeLine(%q) = %q, want none", line, w)
			}
		}
		for line, want := range map[string]string{"Loss.": "Loss", "Verdict: tie": "tie", "Outcome: **win**": "win", "win — after the fix": "win", "Veredito: derrota": "derrota", "Derrota técnica": "Derrota"} {
			if w := doclang.OutcomeLine(line); w != want {
				t.Errorf("OutcomeLine(%q) = %q, want %q", line, w, want)
			}
		}
	})
	t.Run("prose does not open a round", func(t *testing.T) {
		for _, line := range []string{"Round 2 of benchmarking was skipped", "Round 10 times and average", "Rounding 2 digits"} {
			if _, ok := doclang.RoundAnchor(line); ok {
				t.Errorf("RoundAnchor(%q) opened a round", line)
			}
		}
		for line, want := range map[string]int{"Round 2 — win: \"x\"": 2, "Round 3": 3, "Round 4: tie": 4, "Round 5 (new judge) — loss": 5, "Rodada 6 de novo": 6} {
			if n, ok := doclang.RoundAnchor(line); !ok || n != want {
				t.Errorf("RoundAnchor(%q) = %d, %v; want %d", line, n, ok, want)
			}
		}
	})
	t.Run("an article is not the judge's letter", func(t *testing.T) {
		if doclang.VerdictIn("Verdict: A clear improvement over the bar") {
			t.Error("\"Verdict: A clear improvement\" read as the literal verdict A")
		}
		for _, text := range []string{"VERDICT: A", "the judge ended with \"VERDICT: B\".", "Verdict: TIE", "VEREDITO: EMPATE"} {
			if !doclang.VerdictIn(text) {
				t.Errorf("doclang.VerdictIn(%q) = false", text)
			}
		}
	})
	t.Run("an outcome word in prose is not a verdict", func(t *testing.T) {
		// Quoted, but the round never says who won: the block mentions "tie"
		// and "win" only in passing.
		section := "Bar: internal/x.go\nRound 1: \"we tie the two outputs together to compare\"; a quick win for readability\n"
		if got := parseGauntlet(section).problem(); got != "rodada 1 sem veredito" {
			t.Errorf("problem = %q, want the missing verdict", got)
		}
	})
	t.Run("the typographic apostrophe", func(t *testing.T) {
		section := "Bar: internal/x.go\nCritic’s findings: 2 — one called it \"a win\"\n"
		if got := parseGauntlet(section).problem(); got != "rodada sem citação do juiz" {
			t.Errorf("problem = %q: the critic's line was read as a round", got)
		}
	})
}

func TestResultEvidence_EnglishIsStrict(t *testing.T) {
	// "subcriterion 3" is another word; Portuguese always matched inside one.
	if got := resultMentions([]byte("see subcriterion 3 and Criteria 1 or 2")); got[3] || !got[1] || !got[2] {
		t.Errorf("mentions = %v", got)
	}
	// Each language joins its own numbers with its own words.
	if got := resultMentions([]byte("critérios 1 to 3")); got[2] || got[3] {
		t.Errorf("a Portuguese citation expanded an English range: %v", got)
	}
}
