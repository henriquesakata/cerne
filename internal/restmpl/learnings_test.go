package restmpl_test

import (
	"reflect"
	"testing"

	"github.com/henriquesakata/cerne/internal/restmpl"
)

func TestLearningItems(t *testing.T) {
	tests := []struct {
		name, result string
		want         []string
	}{
		{"list with subitems", "## Resultado\nok\n\n## Aprendizados e desvios\n- first\n  - sub a\n  - sub b\n- second\n\n## Gauntlet\nBarra: x\n",
			[]string{"first\n  - sub a\n  - sub b", "second"}},
		{"prose only", "## Aprendizados e desvios\nOne paragraph.\n\nAnother one.\n\n## Gauntlet\n",
			[]string{"One paragraph.\n\nAnother one."}},
		{"numbered", "## Aprendizados e desvios\n1. one\n2. two\n", []string{"one", "two"}},
		{"template comment only", "## Aprendizados e desvios\n<!-- O que aprendeu -->\n\n## Gauntlet\n", nil},
		{"comment then list", "## Aprendizados e desvios\n<!-- guide\n- not an item -->\n- real\n", []string{"real"}},
		{"english result", "## Learnings and deviations\n- one\n\n## Gauntlet\n", []string{"one"}},
		{"english marker", "## Learnings and deviations\nNone.\n", nil},
		{"section absent", "## Resultado\nok\n", nil},
		{"last section, no trailing heading", "## Aprendizados e desvios\n- tail", []string{"tail"}},
		{"bullet marker skipped, real kept", "## Aprendizados e desvios\n- N/A\n- real\n", []string{"real"}},
		{"preamble ignored when a list exists", "## Aprendizados e desvios\nIntro line.\n- item\n", []string{"item"}},
		{"list in a code fence is text", "## Aprendizados e desvios\n```\n- x\n```\n", []string{"```\n- x\n```"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := restmpl.LearningItems(tt.result); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	for _, m := range []string{"N/A", "n/a", "Nenhum", "Nenhuma", "Nada", "None", "Nothing", "-", "N/A.", "nenhum.", "**None**", "none", "Nothing."} {
		for _, form := range []string{"## Aprendizados e desvios\n" + m + "\n", "## Learnings and deviations\n- " + m + "\n"} {
			if got := restmpl.LearningItems(form); got != nil {
				t.Errorf("marker %q in %q: got %q, want none", m, form, got)
			}
		}
	}
}
