package runtmpl_test

import (
	"reflect"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/runtmpl"
	"gitlab.com/rockt/cerne/internal/spectmpl"
)

// TestSectionsCanonical pins the canonical sections and their order. Ordering is
// load-bearing (trigger/prereqs/procedure first, troubleshooting last).
func TestSectionsCanonical(t *testing.T) {
	want := []string{
		"Quando usar",
		"Pré-requisitos",
		"Procedimento",
		"Verificação",
		"Rollback e troubleshooting",
	}
	if !reflect.DeepEqual(runtmpl.Sections, want) {
		t.Fatalf("Sections = %v, want %v", runtmpl.Sections, want)
	}
}

// TestBodyHasSectionsInOrder ensures every canonical section is present in the
// template body as a level-2 heading, in the Sections order.
func TestBodyHasSectionsInOrder(t *testing.T) {
	prev := -1
	for _, s := range runtmpl.Sections {
		i := strings.Index(runtmpl.Body, "## "+s)
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
// and the runbook-specific rules: expected output per step, right-sizing,
// escalation, and status-is-data.
func TestBodyHasGuidanceMarkers(t *testing.T) {
	wants := []string{
		spectmpl.AmbiguityMarker, // same marker across the lifecycle
		"saída esperada",         // commands show expected output
		"Dimensione pela tarefa", // auto-sizing / zero ceremony
		"a quem escalar",         // escalation path
		"gerido pelo cerne",      // status is data, not prose
	}
	for _, want := range wants {
		if !strings.Contains(runtmpl.Body, want) {
			t.Fatalf("Body missing guidance %q", want)
		}
	}
}

// TestStub composes the scaffold cerne writes for a new runbook: title heading
// then the template body.
func TestStub(t *testing.T) {
	s := runtmpl.Stub("  Recuperar dev env  ")
	if !strings.HasPrefix(s, "# Recuperar dev env\n\n") {
		t.Fatalf("Stub should start with the trimmed title heading:\n%s", s)
	}
	if !strings.Contains(s, runtmpl.Body) {
		t.Fatalf("Stub should embed the template Body")
	}
}

// TestStubReadsAsStub keeps the scaffolded runbook a stub until the author fills
// it in (consistent with the rest of the family).
func TestStubReadsAsStub(t *testing.T) {
	doc := "---\ncerne_id: RB-0001\n---\n\n" + runtmpl.Stub("Recuperar dev env")
	if doccheck.HasRealContent([]byte(doc)) {
		t.Fatalf("a scaffolded runbook stub must read as a stub:\n%s", doc)
	}
}
