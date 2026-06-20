package spectmpl_test

import (
	"reflect"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/spectmpl"
)

// TestSectionsCanonical pins the canonical sections and their order. Ordering is
// load-bearing (problem/criteria first, verification last), so it is asserted.
func TestSectionsCanonical(t *testing.T) {
	want := []string{
		"Problema",
		"Objetivo e critérios de aceitação",
		"Decisões do design",
		"Desenho",
		"Verificação",
		"Fora de escopo",
	}
	if !reflect.DeepEqual(spectmpl.Sections, want) {
		t.Fatalf("Sections = %v, want %v", spectmpl.Sections, want)
	}
}

// TestBodyHasSectionsInOrder ensures every canonical section is present in the
// template body as a level-2 heading, in the Sections order.
func TestBodyHasSectionsInOrder(t *testing.T) {
	prev := -1
	for _, s := range spectmpl.Sections {
		i := strings.Index(spectmpl.Body, "## "+s)
		if i < 0 {
			t.Fatalf("Body missing section heading %q", s)
		}
		if i < prev {
			t.Fatalf("section %q appears out of canonical order", s)
		}
		prev = i
	}
}

// TestBodyHasGuidanceMarkers checks the body teaches the ambiguity marker and
// the EARS phrasing for behavioural criteria.
func TestBodyHasGuidanceMarkers(t *testing.T) {
	for _, want := range []string{spectmpl.AmbiguityMarker, "O SISTEMA DEVE"} {
		if !strings.Contains(spectmpl.Body, want) {
			t.Fatalf("Body missing guidance %q", want)
		}
	}
}

// TestStub composes the scaffold cerne writes for a new feature: title heading
// then the template body.
func TestStub(t *testing.T) {
	s := spectmpl.Stub("  Login com Google  ")
	if !strings.HasPrefix(s, "# Login com Google\n\n") {
		t.Fatalf("Stub should start with the trimmed title heading:\n%s", s)
	}
	if !strings.Contains(s, spectmpl.Body) {
		t.Fatalf("Stub should embed the template Body")
	}
}
