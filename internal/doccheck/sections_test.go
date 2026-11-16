package doccheck

import (
	"reflect"
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
