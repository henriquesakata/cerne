package app

import (
	"regexp"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// englishWord is a function word or a domain word of the English examples the
// prompts used to dictate inside a pt-BR document.
var englishWord = regexp.MustCompile(`(?i)\b(the|of|by|and|with|win|tie|loss|same|class|guide|human|comparison|critic|fresh|context|stopping|recorded|test|benchmark|invariant|opens|only)\b`)

// placeholder is an <instruction> the model replaces: it is the prompt's own words.
var placeholder = regexp.MustCompile(`<[^>]*>`)

// F-0447: a line the model must copy into the document — the bar, method and
// exit lines, the filled example, the alternative-standard form — carries the
// free text of the project's language, never an English sentence.
func TestGauntletContractCopyLinesAreInTheProjectLanguage(t *testing.T) {
	for _, l := range doclang.Langs() {
		var copied []string
		for _, line := range strings.Split(GauntletContract(l), "\n") {
			line = strings.TrimSpace(line)
			for _, t := range []doclang.Term{doclang.BarLabel, doclang.MethodLabel, doclang.ExitLabel} {
				if i := strings.Index(line, doclang.Display(t, l)+":"); i >= 0 && !strings.HasPrefix(line, "- ") {
					copied = append(copied, placeholder.ReplaceAllString(line[i:], ""))
					break
				}
			}
		}
		if len(copied) < 6 {
			t.Fatalf("%s: found %d copy lines, want the 3 header lines, the example and the two exemptions:\n%v", l, len(copied), copied)
		}
		if g := GauntletContract(l); !strings.Contains(g, doclang.Examples(l).Alternative) || !strings.Contains(g, doclang.Examples(l).Method) {
			t.Errorf("%s: the contract lacks the language's alternative standard or method text", l)
		}
		if l != doclang.PT {
			continue
		}
		for _, line := range copied {
			line = strings.ReplaceAll(line, "Kiro", "")
			if w := englishWord.FindString(line); w != "" {
				t.Errorf("pt copy line carries the English word %q: %s", w, line)
			}
		}
	}
}

// F-0447: the tokens a roast reply carries — the severity set, the empty-report
// sentence and the coverage-matrix verdicts — are the project's language's, so
// a pt-BR report reads as it did before the prompts went English. No code
// parses them (the report is shown as text); the reply the prompt asks for is
// what the report prints.
func TestRoastPromptOutputTokensFollowTheProjectLanguage(t *testing.T) {
	for _, l := range doclang.Langs() {
		ex := doclang.Examples(l)
		for _, p := range append(append([]RoastPersona{}, RoastPersonas...), PlanRoastPersonas...) {
			prompt := BuildRoastPromptFor(l, p, "F-0001", "T", SpecRoastIntro, []RoastDoc{{Label: "SPEC", Body: "b"}})
			for _, want := range []string{ex.Severities, `"` + ex.NoFindings + `"`} {
				if !strings.Contains(prompt, want) {
					t.Errorf("%s/%s: the prompt lacks %q", l, p.Key, want)
				}
			}
			if p.Matrix != "" {
				c := doclang.Display(doclang.Criterion, l)
				for _, want := range []string{c + " N: " + ex.Covered, c + " N: " + ex.Uncovered} {
					if !strings.Contains(prompt, want) {
						t.Errorf("%s/%s: the matrix lacks %q", l, p.Key, want)
					}
				}
			}
			if l == doclang.PT && strings.Contains(prompt, "[parallel]") {
				t.Errorf("%s/%s: the pt prompt names the English parallel mark", l, p.Key)
			}
		}
	}
}

// F-0447: the codex live-query block names the answer headings of the project's
// language only, never the other's.
func TestChatCodexLiveQueryHeadingsFollowTheProjectLanguage(t *testing.T) {
	for _, l := range doclang.Langs() {
		ex, other := doclang.Examples(l), doclang.Examples(map[doclang.Lang]doclang.Lang{doclang.PT: doclang.EN, doclang.EN: doclang.PT}[l])
		q := ChatCodexLiveQuery(l)
		if !strings.Contains(q, `"`+ex.WhereIs+`", "`+ex.HowToAdjust+`", "`+ex.SourcesRead+`"`) {
			t.Errorf("%s: the block lacks its headings", l)
		}
		if strings.Contains(q, other.WhereIs) || strings.Contains(q, other.SourcesRead) {
			t.Errorf("%s: the block names the other language's headings", l)
		}
	}
}
