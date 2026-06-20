package dectmpl_test

import (
	"reflect"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/dectmpl"
	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/spectmpl"
)

// TestSectionsCanonical pins the canonical sections and their order. Ordering is
// load-bearing (context/decision first, implementation/relations last), so it is
// asserted.
func TestSectionsCanonical(t *testing.T) {
	want := []string{
		"Contexto",
		"Decisão",
		"Consequências",
		"Alternativas consideradas",
		"Implementação e relações",
	}
	if !reflect.DeepEqual(dectmpl.Sections, want) {
		t.Fatalf("Sections = %v, want %v", dectmpl.Sections, want)
	}
}

// TestBodyHasSectionsInOrder ensures every canonical section is present in the
// template body as a level-2 heading, in the Sections order.
func TestBodyHasSectionsInOrder(t *testing.T) {
	prev := -1
	for _, s := range dectmpl.Sections {
		i := strings.Index(dectmpl.Body, "## "+s)
		if i < 0 {
			t.Fatalf("Body missing section heading %q", s)
		}
		if i < prev {
			t.Fatalf("section %q appears out of canonical order", s)
		}
		prev = i
	}
}

// TestBodyHasGuidanceMarkers checks the body teaches the shared ambiguity marker
// and the ADR-specific authoring rules: imperative decision, always-list-the
// negatives, why-rejected alternatives, right-sizing, and status-is-data.
func TestBodyHasGuidanceMarkers(t *testing.T) {
	wants := []string{
		spectmpl.AmbiguityMarker,  // same marker across the lifecycle
		"imperativo",              // decision stated imperatively
		"NEGATIVAS",               // negative consequences are mandatory
		"POR QUE foi rejeitada",   // alternatives with rationale
		"Dimensione pela decisão", // auto-sizing / zero ceremony
		"gerido pelo cerne",       // status is data, not prose
		"não a pergunta",          // title records the decision, not the question
		"registro histórico",      // an accepted ADR is immutable — supersede, don't rewrite
	}
	for _, want := range wants {
		if !strings.Contains(dectmpl.Body, want) {
			t.Fatalf("Body missing guidance %q", want)
		}
	}
}

// TestStub composes the scaffold cerne writes for a new decision: title heading
// then the template body.
func TestStub(t *testing.T) {
	s := dectmpl.Stub("  Adotar micro-frontends  ")
	if !strings.HasPrefix(s, "# Adotar micro-frontends\n\n") {
		t.Fatalf("Stub should start with the trimmed title heading:\n%s", s)
	}
	if !strings.Contains(s, dectmpl.Body) {
		t.Fatalf("Stub should embed the template Body")
	}
}

// TestStubReadsAsStub keeps the scaffolded ADR a stub so it reads as pending
// (consistent with spec/plan) until the author fills it in.
func TestStubReadsAsStub(t *testing.T) {
	doc := "---\ncerne_id: D-0001\n---\n\n" + dectmpl.Stub("Adotar micro-frontends")
	if doccheck.HasRealContent([]byte(doc)) {
		t.Fatalf("a scaffolded ADR stub must read as a stub:\n%s", doc)
	}
}
