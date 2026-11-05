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
		"Execução recomendada (agente)",
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

// TestBodyHasAgentExecutionSection pins F-0207 criterion 1 ("Execução
// recomendada (agente)" sits between Verificação and Fora de escopo) plus the
// F-0266 reversal: the section is now IN Sections too, so incomplete-doc
// charges it. F-0207 had kept it Body-only to spare pre-template specs; that
// motive died once the rule started scanning live features only — history is
// never checked.
func TestBodyHasAgentExecutionSection(t *testing.T) {
	verificacao := strings.Index(spectmpl.Body, "## Verificação")
	agente := strings.Index(spectmpl.Body, "## Execução recomendada (agente)")
	foraDeEscopo := strings.Index(spectmpl.Body, "## Fora de escopo")
	if agente < 0 {
		t.Fatalf("Body missing '## Execução recomendada (agente)' section")
	}
	if !(verificacao < agente && agente < foraDeEscopo) {
		t.Fatalf("agent-execution section out of place (verificacao=%d agente=%d foraDeEscopo=%d)", verificacao, agente, foraDeEscopo)
	}
	want := []string{"Verificação", "Execução recomendada (agente)", "Fora de escopo"}
	if got := spectmpl.Sections[len(spectmpl.Sections)-3:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("Sections tail = %v, want %v (F-0266)", got, want)
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

// TestBodyDropsStatusField pins F-0280 criterion 1: the METADADOS block no
// longer asks for a **Status:** line — lifecycle state is data on the card
// (cerne status), and duplicating it in prose is the "documentation that lies"
// problem the founding spec exists to kill.
func TestBodyDropsStatusField(t *testing.T) {
	if strings.Contains(spectmpl.Body, "Status:") {
		t.Fatalf("Body must not ask for a Status: metadata line:\n%s", spectmpl.Body)
	}
	if !strings.Contains(spectmpl.Body, "cerne status") {
		t.Fatalf("Body should point the reader at the state as data (cerne status):\n%s", spectmpl.Body)
	}
}

// TestBodyHasOrigemExample pins F-0280 criterion 2: the Origem: guidance ships a
// literal filled line, quoted, in the shape the author copies — the field the
// blind judges called the weakest for having categories but no form. The example
// is exported so the draft prompt derives from it instead of restating it.
func TestBodyHasOrigemExample(t *testing.T) {
	if !strings.HasPrefix(spectmpl.OrigemExample, `"`) || !strings.HasSuffix(spectmpl.OrigemExample, `"`) {
		t.Fatalf("OrigemExample must be a quoted literal line, got %q", spectmpl.OrigemExample)
	}
	// Anchored on the guidance lines (line starts), not the header that
	// enumerates the fields — the header names them all before any guidance.
	origem := strings.Index(spectmpl.Body, "\nOrigem:")
	nivel := strings.Index(spectmpl.Body, "\nNível:")
	example := strings.Index(spectmpl.Body, spectmpl.OrigemExample)
	if origem < 0 || example < 0 {
		t.Fatalf("Body missing the Origem: guidance or its example:\n%s", spectmpl.Body)
	}
	if !(origem < example && example < nivel) {
		t.Fatalf("the Origem example must sit inside the Origem guidance (origem=%d example=%d nivel=%d)", origem, example, nivel)
	}
}

// TestBodyHasUnknownFieldRule pins F-0280 criterion 3: the METADADOS block tells
// the author what to write when a value is not known, and forbids guessing —
// the same "desconhecido" the result template already uses (restmpl), so the two
// halves of the family speak one vocabulary.
func TestBodyHasUnknownFieldRule(t *testing.T) {
	if spectmpl.UnknownMarker != "desconhecido" {
		t.Fatalf("UnknownMarker = %q, want the restmpl vocabulary %q", spectmpl.UnknownMarker, "desconhecido")
	}
	for _, want := range []string{spectmpl.UnknownMarker, "palpite"} {
		if !strings.Contains(spectmpl.Body, want) {
			t.Fatalf("Body missing the unknown-field rule token %q:\n%s", want, spectmpl.Body)
		}
	}
}

// TestStubBarFormsNeedNoPriorConfirmation (F-0308): the "não aplicável"
// justification is a one-line proposal the human approve confirms — the
// template no longer asks for it to be confirmed beforehand.
func TestStubBarFormsNeedNoPriorConfirmation(t *testing.T) {
	s := spectmpl.Stub("t")
	if !strings.Contains(s, "aplicável (<justificativa de 1 linha>)") {
		t.Fatalf("stub lacks the one-line justification form:\n%s", s)
	}
	if strings.Contains(s, "justificativa confirmada") {
		t.Fatalf("stub still asks for a confirmed justification:\n%s", s)
	}
}
