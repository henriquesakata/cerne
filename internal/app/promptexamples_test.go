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
