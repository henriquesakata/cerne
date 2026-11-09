package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// F-0874 r1 (M5): a pending/manual precondition of a step already ok leaves the
// warning. S1 declares a manual line; once S1 is ok it must not show at S2.
func TestNextDropsOpenPreconditionOfFinishedStep(t *testing.T) {
	projDir, _, _ := setupProject(t)
	f := filepath.Join(t.TempDir(), "rb.md")
	mustWrite(t, f, "# Rb\n\n- S1 prepara\n  Pré-condição S1: ninguem mexeu na janela\n- S2 aplica\n  Pré-condição S2: registro D-9999\n")
	rb(t, projDir, "runbook", "new", "Rb", "--file", f)
	id := strings.Fields(rb(t, projDir, "runbook", "run", "RB-0001", "--janela", "J1"))[0]
	next := rb(t, projDir, "next")
	if !strings.Contains(next, "ninguem mexeu na janela") {
		t.Fatalf("S1's manual line must show at S1:\n%s", next)
	}
	rb(t, projDir, "runbook", "step", id, "S1", "--ok", "feito")
	next = rb(t, projDir, "next")
	if !strings.Contains(next, "next step S2") || strings.Contains(next, "ninguem mexeu na janela") || !strings.Contains(next, "D-9999 not found") {
		t.Fatalf("S1 is ok: only S2's open line stays:\n%s", next)
	}
}

// F-0874 r1: a hostile declaration shows as manual and says it was not run.
func TestPreflightShowsInvalidDeclarationAsManual(t *testing.T) {
	projDir, _, _ := setupProject(t)
	f := filepath.Join(t.TempDir(), "rb.md")
	mustWrite(t, f, "# Rb\n\n- S1 a\n  Pré-condição S1: mr x!--web merged\n  Pré-condição S1: result ../x\n")
	rb(t, projDir, "runbook", "new", "Rb", "--file", f)
	out := rb(t, projDir, "runbook", "preflight", "RB-0001")
	if !strings.Contains(out, "0 met, 0 pending, 2 manual") || strings.Count(out, "invalid declaration") != 2 {
		t.Fatalf("both must be manual, flagged invalid:\n%s", out)
	}
}
