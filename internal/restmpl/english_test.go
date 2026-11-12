package restmpl

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/doclang"
)

var headingRe = regexp.MustCompile(`(?m)^## (.+)$`)

func headings(body string) []string {
	var out []string
	for _, m := range headingRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// F-0359 criterion 1: the English stub carries the English sections, in
// template order, and none of the Portuguese vocabulary.
func TestStub_English(t *testing.T) {
	stub := StubIn(doclang.EN, "Title")
	if got, want := headings(stub), doclang.Sections(doclang.Result, doclang.EN); !slices.Equal(got, want) {
		t.Fatalf("headings = %q, want %q", got, want)
	}
	for _, pt := range []string{"COMO USAR", "METADADOS", "[ESCLARECER", "Barra:", "Rodada", "critério"} {
		if strings.Contains(stub, pt) {
			t.Errorf("English stub contains the Portuguese token %q", pt)
		}
	}
	if !strings.Contains(stub, "[CLARIFY: ") {
		t.Error("English stub does not cite the [CLARIFY: marker")
	}
	// Guidance is scaffold: a fresh English stub must still read as a stub, or
	// the lifecycle gates would open on an empty document.
	if doccheck.HasRealContent([]byte(stub)) {
		t.Error("a fresh English stub reads as real content")
	}
}

// F-0359 criterion 2: Portuguese is byte-identical to what Stub always wrote.
func TestStub_PortugueseUnchanged(t *testing.T) {
	if StubIn(doclang.PT, "Título") != Stub("Título") {
		t.Error("StubIn(PT) differs from Stub")
	}
	if got, want := headings(Body), Sections; !slices.Equal(got, want) {
		t.Errorf("Portuguese headings = %q, want %q", got, want)
	}
}
