package lint

import (
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// Gauntlet r1: a runbook erratum left as a proposal, or decided but not merged,
// is flagged — the state must not stay parked silently.
func TestLintFlagsPendingAndUnmergedRunbookErrata(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunbook(dir, store.Runbook{ID: "RB-0001", Title: "Giro", State: domain.RunbookActive}); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(0, 0).UTC()
	for _, txt := range []string{"Altera: S1\n\nA", "Altera: S1\nEstado: decidida — \"ok\" (dono, 2026-11-09)\n\nB", "Altera: S1\nEstado: decidida — \"ok\" (dono, 2026-11-09)\nFundida: revisão 2\n\nC"} {
		if err := store.AppendErrata(dir, "RB-0001", txt, "ag", at); err != nil {
			t.Fatal(err)
		}
	}
	findings, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []Finding
	for _, f := range findings {
		if f.Rule == RuleErrataPending {
			got = append(got, f)
		}
	}
	if len(got) != 2 || !strings.Contains(got[0].Detail, "E1") || !strings.Contains(got[1].Detail, "E2") ||
		!strings.Contains(got[0].Fix, "decide") || !strings.Contains(got[1].Fix, "merge") {
		t.Fatalf("want E1 (decide) and E2 (merge) flagged, E3 not: %+v", got)
	}
}

// F-0345 crit. 7: an open item with no step and no `Sem passo: sim` is flagged
// with its own detail and the way out; a deliberate step-less one keeps the
// plain proposal finding; a decided step-less one is retired and silent.
func TestLintFlagsStepLessErratum(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunbook(dir, store.Runbook{ID: "RB-0001", Title: "Giro", State: domain.RunbookActive}); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(0, 0).UTC()
	for _, txt := range []string{
		"Altera: -\n\nEsquecida.",
		"Altera: -\nSem passo: sim\n\nDe propósito.",
		"Altera: -\nEstado: decidida — \"ok\" (dono, 2026-11-09)\n\nAposentada.",
	} {
		if err := store.AppendErrata(dir, "RB-0001", txt, "ag", at); err != nil {
			t.Fatal(err)
		}
	}
	findings, err := Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []Finding
	for _, f := range findings {
		if f.Rule == RuleErrataPending {
			got = append(got, f)
		}
	}
	if len(got) != 2 {
		t.Fatalf("want E1 (no step) and E2 (proposal) only: %+v", got)
	}
	if want := "erratum E1 de RB-0001 não declara passo: o --consolidado não o põe no lugar e o merge não o funde"; got[0].Detail != want {
		t.Fatalf("detail = %q, want %q", got[0].Detail, want)
	}
	if !strings.Contains(got[0].Fix, "cerne runbook errata RB-0001 --altera <passo>") || !strings.Contains(got[0].Fix, "cerne runbook decide RB-0001 E1") {
		t.Fatalf("fix must rewrite with --altera and then decide the old item: %q", got[0].Fix)
	}
	if !strings.Contains(got[1].Detail, "E2") || !strings.Contains(got[1].Detail, "proposta") {
		t.Fatalf("a deliberate step-less item keeps the proposal finding: %+v", got[1])
	}
}

// Gauntlet r2: an inactive runbook is not charged for parked errata.
func TestLintErrataPendingSkipsInactiveRunbook(t *testing.T) {
	dir := t.TempDir()
	if err := store.InitLayout(dir, store.Project{Name: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunbook(dir, store.Runbook{ID: "RB-0001", Title: "Velho", State: domain.RunbookSuperseded}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunbook(dir, store.Runbook{ID: "RB-0002", Title: "Fora", State: domain.RunbookState("archived")}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"RB-0001", "RB-0002"} {
		if err := store.AppendErrata(dir, id, "Altera: S1\n\nA", "ag", time.Unix(0, 0).UTC()); err != nil {
			t.Fatal(err)
		}
	}
	findings, _ := Run(dir)
	for _, f := range findings {
		if f.Rule == RuleErrataPending {
			t.Fatalf("inactive runbook charged: %+v", f)
		}
	}
}
