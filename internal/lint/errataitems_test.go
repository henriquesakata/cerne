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
