package plantmpl_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/plantmpl"
	"github.com/henriquesakata/cerne/internal/spectmpl"
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

// TestStubForEpicNoChildren: with no children StubForEpic must be
// byte-identical to Stub (spec criterion 2 — no change for a plain feature).
func TestStubForEpicNoChildren(t *testing.T) {
	title := "Conciliação bancária"
	if got, want := plantmpl.StubForEpic(title, nil), plantmpl.Stub(title); got != want {
		t.Fatalf("StubForEpic with no children must equal Stub:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestStubForEpicWithChildren pins the canonical execution-map format
// scaffolded into "Dependências e ordem": one numbered step per child, in
// order, plus the authoring TODO guide (spec criterion 1).
func TestStubForEpicWithChildren(t *testing.T) {
	children := []plantmpl.EpicChild{
		{ID: "T-0001", Title: "room manager no server"},
		{ID: "T-0003", Title: "UI WarRoom"},
		{ID: "T-0004", Title: "CLI cerne room"},
	}
	s := plantmpl.StubForEpic("Épico X", children)

	if !strings.Contains(s, "## Dependências e ordem") {
		t.Fatalf("missing section heading:\n%s", s)
	}
	wants := []string{
		"Etapa 1: T-0001 (room manager no server)",
		"Etapa 2: T-0003 (UI WarRoom)",
		"Etapa 3: T-0004 (CLI cerne room)",
		"TODO: agrupe em etapas — mesma etapa = paralelas",
	}
	for _, want := range wants {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	// the map must land inside "Dependências e ordem", before the next section.
	depIdx := strings.Index(s, "## Dependências e ordem")
	nextIdx := strings.Index(s, "## Riscos e rollback")
	mapIdx := strings.Index(s, "Etapa 1:")
	if !(depIdx < mapIdx && mapIdx < nextIdx) {
		t.Fatalf("execution map must sit inside Dependências e ordem, before Riscos e rollback:\n%s", s)
	}
}

// TestStubForEpicWithChildrenReadsAsStub: the epic scaffold — TODO map
// included — still counts as a stub until the author actually fills a
// section (spec criterion 4, doccheck gate).
func TestStubForEpicWithChildrenReadsAsStub(t *testing.T) {
	children := []plantmpl.EpicChild{{ID: "T-0001", Title: "x"}}
	doc := "---\ncerne_id: F-0001\n---\n\n" + plantmpl.StubForEpic("Épico X", children)
	if doccheck.HasRealContent([]byte(doc)) {
		t.Fatalf("epic plan scaffold must still read as a stub:\n%s", doc)
	}
}

// TestBodyDropsStatusField pins F-0338 criterion 12: like the spec template
// (spectmpl), the plan METADADOS no longer ask for a **Status:** line — the
// state is data on the card, and a copy in prose goes stale. The other
// metadata fields stay.
func TestBodyDropsStatusField(t *testing.T) {
	if strings.Contains(plantmpl.Body, "Status:") {
		t.Fatalf("Body must not ask for a Status: metadata line:\n%s", plantmpl.Body)
	}
	if !strings.Contains(plantmpl.Body, "**Spec:** / **Branch:** / **Saída (Gauntlet):**") {
		t.Fatalf("Body must keep the Spec / Branch / Saída (Gauntlet) metadata fields:\n%s", plantmpl.Body)
	}
}

// TestBodyCarriesGauntletExit pins F-0275 criterion 6: the plan scaffold asks
// for the Gauntlet exit — inherit the spec's bar, or say in one line why the
// loop does not apply — as METADATA guidance, never as a canonical section.
// Adding it to Sections would make every existing plan incomplete-doc, which
// is the enforcement the criterion explicitly excludes.
func TestBodyCarriesGauntletExit(t *testing.T) {
	for _, want := range []string{"Saída (Gauntlet)", "herde a barra da spec", "não se aplica"} {
		if !strings.Contains(plantmpl.Body, want) {
			t.Errorf("plan template missing %q", want)
		}
	}
	for _, s := range plantmpl.Sections {
		if strings.Contains(s, "Gauntlet") {
			t.Fatalf("Gauntlet must stay out of Sections (it would become a lint gate): %v", plantmpl.Sections)
		}
	}
	if len(plantmpl.Sections) != 6 {
		t.Fatalf("Sections = %d, want the 6 canonical plan sections unchanged: %v", len(plantmpl.Sections), plantmpl.Sections)
	}
}
