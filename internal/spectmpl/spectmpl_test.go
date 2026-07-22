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

// TestBodyHasCapabilityLevelGuidance pins F-0194: the METADADOS block teaches
// the suggested-capability-level line with the generic vocabulary, and the
// vocabulary itself is exported so the draft prompt derives from it (no drift).
func TestBodyHasCapabilityLevelGuidance(t *testing.T) {
	wantLevels := []string{"leve", "padrão", "avançado"}
	if !reflect.DeepEqual(spectmpl.Levels, wantLevels) {
		t.Fatalf("Levels = %v, want %v", spectmpl.Levels, wantLevels)
	}
	if !strings.Contains(spectmpl.Body, "Nível:") {
		t.Fatalf("Body missing the Nível: guidance line")
	}
	for _, l := range spectmpl.Levels {
		if !strings.Contains(spectmpl.Body, l) {
			t.Fatalf("Body missing level %q in the guidance", l)
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

// TestStubWithContext seeds the Problema section with the creation-time
// context (F-0120): the text lands after the Problema heading and before the
// next section, with every canonical section intact.
func TestStubWithContext(t *testing.T) {
	ctx := "Agentes registram a feature e não voltam para a spec."
	s := spectmpl.StubWithContext("Captura de contexto", "  "+ctx+"  ")
	problema := strings.Index(s, "## Problema")
	objetivo := strings.Index(s, "## Objetivo")
	seeded := strings.Index(s, ctx)
	if seeded < 0 {
		t.Fatalf("StubWithContext should contain the context text:\n%s", s)
	}
	if !(problema < seeded && seeded < objetivo) {
		t.Fatalf("context should sit inside the Problema section (problema=%d seeded=%d objetivo=%d)", problema, seeded, objetivo)
	}
	for _, sec := range spectmpl.Sections {
		if !strings.Contains(s, "## "+sec) {
			t.Fatalf("StubWithContext lost section %q", sec)
		}
	}
}

// TestStubWithContextEnglishUsesEnMarker pins F-0182 criterion 10: a scaffold
// seeded with English context carries the en vocabulary in the marker
// guidance, so the agent filling it authors [CLARIFY: …] naturally. The pt
// path stays byte-identical to before.
func TestStubWithContextEnglishUsesEnMarker(t *testing.T) {
	ctx := "The provisioning path does not know about the host and needs a way to validate the credentials that already exist."
	s := spectmpl.StubWithContext("Self-hosted target", ctx)
	if !strings.Contains(s, "[CLARIFY:") {
		t.Fatalf("English-context scaffold should cite the en marker word:\n%s", s)
	}
	if strings.Contains(s, "[ESCLARECER") {
		t.Fatalf("English-context scaffold should not keep the pt marker word:\n%s", s)
	}
	// pt context keeps the pt vocabulary untouched.
	pt := spectmpl.StubWithContext("Alvo self-hosted", "O provisionamento não conhece o host e precisa de um caminho para validar as credenciais que já existem.")
	if !strings.Contains(pt, "[ESCLARECER:") || strings.Contains(pt, "[CLARIFY") {
		t.Fatalf("pt-context scaffold should keep the pt marker word:\n%s", pt)
	}
}

// TestStubWithContextEmptyDegradesToStub pins the no-context path.
func TestStubWithContextEmptyDegradesToStub(t *testing.T) {
	if spectmpl.StubWithContext("X", "   ") != spectmpl.Stub("X") {
		t.Fatal("empty context should degrade to the plain Stub")
	}
}
