package doccheck

import (
	"reflect"
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/doclang"
)

func TestMissingSections(t *testing.T) {
	data := []byte("## Context\n\n## Decisão\n")
	want := []string{"Consequences", "Alternatives considered", "Implementation and relations"}
	if got := MissingSections(data, doclang.Decision, doclang.EN); !reflect.DeepEqual(got, want) {
		t.Errorf("missing = %v, want %v", got, want)
	}
}

// A "## Verificação" inside a fenced code block is not a section (F-0435).
func TestMissingSections_IgnoresFencedHeadings(t *testing.T) {
	data := []byte("## Problema\n\n```go\n// ## Verificação\n```\n\n````md\n## Desenho\n```\n## Fora de escopo\n````\n")
	got := MissingSections(data, doclang.Spec, doclang.PT)
	for _, h := range []string{"Verificação", "Desenho", "Fora de escopo"} {
		if !slices.Contains(got, h) {
			t.Errorf("%q counted as present from inside a fence; missing = %v", h, got)
		}
	}
	if slices.Contains(got, "Problema") {
		t.Errorf("a real heading outside the fences must count; missing = %v", got)
	}
}
