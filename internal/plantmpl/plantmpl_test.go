package plantmpl_test

import (
	"reflect"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/plantmpl"
	"gitlab.com/rockt/cerne/internal/spectmpl"
)

// TestSectionsCanonical pins the canonical sections and their order. Ordering is
// load-bearing (strategy/tasks first, definition-of-done last), so it is asserted.
func TestSectionsCanonical(t *testing.T) {
	want := []string{
		"Estratégia",
		"Sequência de tarefas",
		"Dependências e ordem",
		"Riscos e rollback",
		"Restrições para o agente",
		"Definição de pronto",
	}
	if !reflect.DeepEqual(plantmpl.Sections, want) {
		t.Fatalf("Sections = %v, want %v", plantmpl.Sections, want)
	}
}

// TestBodyHasSectionsInOrder ensures every canonical section is present in the
// template body as a level-2 heading, in the Sections order.
func TestBodyHasSectionsInOrder(t *testing.T) {
	prev := -1
	for _, s := range plantmpl.Sections {
		i := strings.Index(plantmpl.Body, "## "+s)
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
// and the plan-specific authoring rules: TDD (test-first), right-sizing (zero
// ceremony), criterion traceability, and the binary definition-of-done.
func TestBodyHasGuidanceMarkers(t *testing.T) {
	wants := []string{
		spectmpl.AmbiguityMarker,       // same marker across the lifecycle
		"Teste primeiro",               // TDD ordering
		"Dimensione pelo trabalho",     // auto-sizing / zero ceremony
		"citando o número do critério", // requirement traceability
		"ATENDIDO/PENDENTE",            // binary, reproducible done-check
	}
	for _, want := range wants {
		if !strings.Contains(plantmpl.Body, want) {
			t.Fatalf("Body missing guidance %q", want)
		}
	}
}

// TestStub composes the scaffold cerne writes when a feature is approved: title
// heading then the template body.
func TestStub(t *testing.T) {
	s := plantmpl.Stub("  Conciliação bancária  ")
	if !strings.HasPrefix(s, "# Conciliação bancária\n\n") {
		t.Fatalf("Stub should start with the trimmed title heading:\n%s", s)
	}
	if !strings.Contains(s, plantmpl.Body) {
		t.Fatalf("Stub should embed the template Body")
	}
}

// TestStubReadsAsStub is the gate-critical property: a freshly scaffolded plan
// (cerne_id frontmatter + the template) must NOT count as real content, so the
// feature start gate stays closed until the author fills it in.
func TestStubReadsAsStub(t *testing.T) {
	doc := "---\ncerne_id: F-0001\n---\n\n" + plantmpl.Stub("Conciliação bancária")
	if doccheck.HasRealContent([]byte(doc)) {
		t.Fatalf("a scaffolded plan stub must read as a stub (gate must stay closed):\n%s", doc)
	}
}

// TestBodyPullsConventionNotes: the template instructs the author to look up
// convention notes for the scope (cerne note tags → note list --component) and
// to cite the applicable N-NNNN ids in the plan (F-0165, criterion 6) — so the
// lookup happens before coding and the plan carries the governing conventions.
func TestBodyPullsConventionNotes(t *testing.T) {
	for _, want := range []string{"cerne note tags", "note list --component", "N-NNNN"} {
		if !strings.Contains(plantmpl.Body, want) {
			t.Errorf("Body missing convention-lookup guidance %q", want)
		}
	}
}
