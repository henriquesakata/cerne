package store

import (
	"strings"
	"testing"
)

const errataFixture = `---
cerne_id: RB-0001
---

## errata 2026-11-06 — agent

Texto legado, sem metadado.

## Nota: subtitulo dentro do bloco

mais texto do item 1.

## errata 2026-11-07 — agent

Altera: S6.6

Regra de parada nova.

## errata 2026-11-08 — dono

Altera: S9.9
Estado: decidida — "pode, faz assim" (dono, 2026-11-09)

Passo que nao existe.
`

func TestParseErrataItems(t *testing.T) {
	items := ParseErrataItems(errataFixture)
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (a '## Nota' inside a block is not a new item)", len(items))
	}
	if items[0].ID != "E1" || items[0].Target != "" || items[0].Decided() || !strings.Contains(items[0].Text, "mais texto do item 1.") {
		t.Fatalf("legacy item: %+v", items[0])
	}
	if items[1].Target != "S6.6" || items[1].Date != "2026-11-07" || items[1].Text != "Regra de parada nova." {
		t.Fatalf("item 2: %+v", items[1])
	}
	it := items[2]
	if !it.Decided() || it.Quote != "pode, faz assim" || it.DecidedBy != "dono" || it.DecidedAt != "2026-11-09" || it.Target != "S9.9" {
		t.Fatalf("decided item: %+v", it)
	}
}

func TestSetErrataMetaDecidesInPlace(t *testing.T) {
	out, err := SetErrataMeta(errataFixture, "E2", "Estado", `decidida — "ok" (ana, 2026-11-09)`)
	if err != nil {
		t.Fatalf("SetErrataMeta: %v", err)
	}
	items := ParseErrataItems(out)
	if len(items) != 3 {
		t.Fatalf("deciding must not create an item: %d items", len(items))
	}
	if !items[1].Decided() || items[1].Quote != "ok" || items[1].Target != "S6.6" || items[1].Text != "Regra de parada nova." {
		t.Fatalf("item 2 after decide: %+v", items[1])
	}
	// Re-deciding replaces the line, it does not stack a second one.
	out, _ = SetErrataMeta(out, "E2", "Estado", `decidida — "outra" (ana, 2026-11-10)`)
	if strings.Count(out, "Estado:") != 2 { // E2 and the fixture's E3
		t.Fatalf("Estado lines = %d, want 2:\n%s", strings.Count(out, "Estado:"), out)
	}
	if _, err := SetErrataMeta(errataFixture, "E9", "Estado", "x"); err == nil {
		t.Fatal("unknown item must error")
	}
}

const bodyFixture = "---\ncerne_id: RB-0001\n---\n\n# Runbook\n\n## S6. Giro\n\nTexto do S6.\n\n### S6.6. Parada\n\n```bash\n# S6.6 num comentario nao e passo\necho x\n```\n\nRegra antiga.\n\n### S6.7. Sonda\n\nOutra coisa.\n\n## S7. Fim\n\nFim.\n"

func TestInsertAtStepsPlacesItemUnderItsStep(t *testing.T) {
	items := ParseErrataItems(errataFixture)
	out, orphans := InsertAtSteps(bodyFixture, items[1:2], func(it ErrataItem) string { return "proposta" })
	i := strings.Index(out, "Errata E2")
	if i < 0 {
		t.Fatalf("item not inserted:\n%s", out)
	}
	if !(strings.Index(out, "Regra antiga.") < i && i < strings.Index(out, "### S6.7.")) {
		t.Fatalf("item must sit at the end of S6.6, before S6.7:\n%s", out)
	}
	if !strings.Contains(out, "2026-11-07") {
		t.Fatalf("item must carry its date:\n%s", out)
	}
	if len(orphans) != 0 {
		t.Fatalf("orphans = %+v", orphans)
	}
}

func TestInsertAtStepsOrphans(t *testing.T) {
	items := ParseErrataItems(errataFixture)
	_, orphans := InsertAtSteps(bodyFixture, items, func(ErrataItem) string { return "x" })
	if len(orphans) != 2 || orphans[0].ID != "E1" || orphans[1].ID != "E3" {
		t.Fatalf("orphans = %+v, want E1 (no Altera) and E3 (step not found)", orphans)
	}
}

// S6.1 must not match inside S6.10 or S6.1.2's neighbours, and a target token
// is matched whole.
func TestStepTokenMatchesWhole(t *testing.T) {
	body := "# R\n\n## S6.10. Dez\n\nx\n\n## S6.1. Um\n\ny\n"
	it := ErrataItem{ID: "E1", Date: "2026-11-07", Target: "S6.1", Text: "t"}
	out, orphans := InsertAtSteps(body, []ErrataItem{it}, func(ErrataItem) string { return "proposta" })
	if len(orphans) != 0 || strings.Index(out, "Errata E1") < strings.Index(out, "## S6.1. Um") {
		t.Fatalf("S6.1 must land under 'S6.1. Um', not S6.10:\n%s", out)
	}
}

// Gauntlet r1: the whole-token clause. S6.1 is neither S6.1.2 nor S6.10 nor
// xS6.1, but does match "S6.1." and "S6.1:" (kills the mutant that drops it).
func TestHasStepToken(t *testing.T) {
	for _, tc := range []struct {
		title string
		want  bool
	}{
		{"S6.1. Um", true}, {"S6.1: um", true}, {"S6.1", true},
		{"S6.1.2. Sub", false}, {"S6.10. Dez", false}, {"xS6.1 um", false}, {"S6.1x", false},
	} {
		if got := hasStepToken(tc.title, "S6.1"); got != tc.want {
			t.Errorf("hasStepToken(%q, S6.1) = %v, want %v", tc.title, got, tc.want)
		}
	}
}

// Gauntlet r1: a heading that only mentions the token in prose is not the step.
func TestInsertAtStepsIgnoresProseMention(t *testing.T) {
	body := "# R\n\n## Notas sobre S6.6\n\nprosa\n\n## S6.6. Parada\n\nreal\n"
	it := ErrataItem{ID: "E1", Date: "2026-11-07", Target: "S6.6", Text: "t"}
	out, orphans := InsertAtSteps(body, []ErrataItem{it}, func(ErrataItem) string { return "proposta" })
	if len(orphans) != 0 || strings.Index(out, "Errata E1") < strings.Index(out, "real") {
		t.Fatalf("E1 must land under the real step S6.6:\n%s", out)
	}
}

// Gauntlet r2 (N7): a title that merely mentions the token after a non-step word
// is not the step, even when the next character is not alphanumeric.
func TestHasStepTokenRefusesProseAndAcceptsRealForms(t *testing.T) {
	for _, tc := range []struct {
		title string
		want  bool
	}{
		{"Veja S6.6", false}, {"Notas: S6.6", false}, {"Notas sobre S6.6", false},
		{"Releases entre o passo S6.6 e o S9", false},
		{"S6.6. Parada", true}, {"Passo S6.6", true}, {"Step S6.6 — x", true},
		{"Etapa S6.6", true}, {"**S6.6**", true}, {"`S6.6` Parada", true},
		{"6. S6.6 — Parada", true}, {"S6.5/S6.6", true}, {"S6.6/S6.7", true},
		{"Fase 3, passo S6.6: expurgo", true}, {"S6.66", false}, {"S6.6.1", false},
	} {
		if got := hasStepToken(tc.title, "S6.6"); got != tc.want {
			t.Errorf("hasStepToken(%q, S6.6) = %v, want %v", tc.title, got, tc.want)
		}
	}
}

// Gauntlet r2 (N10): the replaced decision is read back and shown.
func TestStateLabelShowsPreviousDecisions(t *testing.T) {
	doc := "## errata 2026-11-07 — a\n\nAltera: S6.6\nAnterior: decidida — \"primeira\" (dono, 2026-11-08)\nEstado: decidida — \"segunda\" (chefe, 2026-11-09)\n\nt\n"
	it := ParseErrataItems(doc)[0]
	if len(it.Previous) != 1 || it.Previous[0] != `"primeira" (dono, 2026-11-08)` {
		t.Fatalf("Previous = %q", it.Previous)
	}
	if l := it.StateLabel(); !strings.Contains(l, `"segunda"`) || !strings.Contains(l, `antes: "primeira" (dono, 2026-11-08)`) {
		t.Fatalf("label must show the history: %s", l)
	}
	if l := (ErrataItem{DecidedBy: "x", Quote: "q", DecidedAt: "d"}).StateLabel(); strings.Contains(l, "antes") {
		t.Fatalf("no history, no 'antes': %s", l)
	}
}
