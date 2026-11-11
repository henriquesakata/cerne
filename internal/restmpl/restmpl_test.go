package restmpl_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/restmpl"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestSectionsCanonical pins the canonical sections and their order. Ordering is
// load-bearing (outcome/acceptance first, learnings last), so it is asserted.
func TestSectionsCanonical(t *testing.T) {
	want := []string{
		"Resultado",
		"Aceitação",
		"O que mudou",
		"Débitos e follow-ups",
		"Aprendizados e desvios",
		"Gauntlet",
	}
	if !reflect.DeepEqual(restmpl.Sections, want) {
		t.Fatalf("Sections = %v, want %v", restmpl.Sections, want)
	}
}

// TestBodyHasSectionsInOrder ensures every canonical section is present in the
// template body as a level-2 heading, in the Sections order.
func TestBodyHasSectionsInOrder(t *testing.T) {
	prev := -1
	for _, s := range restmpl.Sections {
		i := strings.Index(restmpl.Body, "## "+s)
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
// and the result-specific rules: evidence-not-assertion, binary acceptance,
// right-sizing, and the complete gate.
func TestBodyHasGuidanceMarkers(t *testing.T) {
	wants := []string{
		spectmpl.AmbiguityMarker,   // same marker across the lifecycle
		"EVIDÊNCIA",                // show evidence, not assertion
		"ATENDIDO/PENDENTE",        // binary, 1:1 acceptance verification
		"1:1 com",                  // tied to the spec criteria
		"Dimensione pelo trabalho", // auto-sizing / zero ceremony
		"feature complete",         // it is the complete gate
	}
	for _, want := range wants {
		if !strings.Contains(restmpl.Body, want) {
			t.Fatalf("Body missing guidance %q", want)
		}
	}
}

// TestStub composes the scaffold cerne writes when a feature starts: title
// heading then the template body.
func TestStub(t *testing.T) {
	s := restmpl.Stub("  Conciliação bancária  ")
	if !strings.HasPrefix(s, "# Conciliação bancária\n\n") {
		t.Fatalf("Stub should start with the trimmed title heading:\n%s", s)
	}
	if !strings.Contains(s, restmpl.Body) {
		t.Fatalf("Stub should embed the template Body")
	}
}

// TestStubReadsAsStub is the gate-critical property: a freshly scaffolded result
// must NOT count as real content, so the feature complete gate stays closed
// until the author fills it in.
func TestStubReadsAsStub(t *testing.T) {
	doc := "---\ncerne_id: F-0001\n---\n\n" + restmpl.Stub("Conciliação bancária")
	if doccheck.HasRealContent([]byte(doc)) {
		t.Fatalf("a scaffolded result stub must read as a stub (gate must stay closed):\n%s", doc)
	}
}

// TestStubCarimboAgente pins the agent stamp (F-0279): the METADADOS block asks
// the author to record which model implemented the feature and at what reasoning
// effort — self-declared, since no harness exposes it. A concrete model id is
// wanted here (history), unlike the spec's generic Nível.
func TestStubCarimboAgente(t *testing.T) {
	s := restmpl.Stub("Conciliação bancária")
	for _, want := range []string{"**Agente:**", "·", "esforço", "humano", "desconhecido"} {
		if !strings.Contains(s, want) {
			t.Errorf("Stub must carry the agent stamp guidance %q:\n%s", want, s)
		}
	}
}

// TestBodyHasGauntletGuidance (F-0303 crit. 8): the template teaches the record
// the complete gate charges — the literal lines, the two literal exemptions and
// the literal "critério N" citation.
func TestBodyHasGauntletGuidance(t *testing.T) {
	for _, want := range []string{
		"## Gauntlet",
		"Barra: <referência nomeada + acesso",
		`Rodada N — <vitória|derrota|empate>: "<citação curta do veredito do juiz>"`,
		"Achados do crítico:",
		"Barra: não aplicável (<justificativa>)",
		"Barra: dispensada por decisão humana (<citação>)",
		`"critério N"`,
		"nunca numa linha",
	} {
		if !strings.Contains(restmpl.Body, want) {
			t.Errorf("Body missing Gauntlet guidance %q", want)
		}
	}
}

// The guide lines are literal forms inside a comment, which the lint reader
// does not skip: an author who fills the other sections and forgets the
// Gauntlet one must still be refused, not vouched for by the leftover guide.
func TestLeftoverGauntletGuideDoesNotVouch(t *testing.T) {
	dir := t.TempDir()
	f := store.Feature{ID: "F-0001", Title: "demo", State: domain.StateInDevelopment}
	if err := store.SaveFeature(dir, f); err != nil {
		t.Fatal(err)
	}
	docDir := filepath.Join(dir, "content", "features", f.ID)
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	result := strings.Replace(restmpl.Stub("demo"), "## Resultado\n", "## Resultado\nEntregue.\n", 1)
	if err := os.WriteFile(filepath.Join(docDir, "result.md"), []byte(result), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, fi := range lint.CheckResult(dir, f) {
		if fi.Rule == lint.RuleResultMissingGauntlet {
			return
		}
	}
	t.Fatalf("an untouched Gauntlet guide counted as a record:\n%s", result)
}
