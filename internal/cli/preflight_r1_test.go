package cli

import (
	"os"
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

// F-0874 r2 (N12): two runs that cite the same MR ask the forge once in one
// `next`. A glab on PATH counts the calls; without the forge shared across the
// runs each would ask, and 3 runs x 2 MRs with the network down cost 30 s.
func TestNextSharesForgeAcrossRuns(t *testing.T) {
	projDir, _, _ := setupProject(t)
	bin := t.TempDir()
	count := filepath.Join(bin, "calls")
	script := "#!/bin/sh\necho x >> '" + count + "'\necho '{\"state\":\"opened\"}'\n"
	mustWrite(t, filepath.Join(bin, "glab"), script)
	if err := os.Chmod(filepath.Join(bin, "glab"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	f := filepath.Join(t.TempDir(), "rb.md")
	mustWrite(t, f, "# Rb\n\n- S1 a\n  Pré-condição S1: mr rockt/hub!7 merged\n")
	rb(t, projDir, "runbook", "new", "Rb", "--file", f)
	rb(t, projDir, "runbook", "run", "RB-0001", "--janela", "J1")
	rb(t, projDir, "runbook", "run", "RB-0001", "--janela", "J2")
	next := rb(t, projDir, "next")
	if strings.Count(next, "rockt/hub!7 is opened") != 2 {
		t.Fatalf("both runs must show the open MR:\n%s", next)
	}
	b, _ := os.ReadFile(count)
	if n := strings.Count(string(b), "x"); n != 1 {
		t.Fatalf("glab asked %d times, want 1 (forge shared across runs)", n)
	}
}

// F-0874 r2 (N17): the precondition-malformed rule reaches `cerne lint`, not
// just the function that computes it.
func TestLintCommandReportsMalformedPrecondition(t *testing.T) {
	projDir, _, _ := setupProject(t)
	f := filepath.Join(t.TempDir(), "rb.md")
	mustWrite(t, f, "# Rb\n\n- S1 a\n  Pré-condição S1: registro F-1 extra\n")
	rb(t, projDir, "runbook", "new", "Rb", "--file", f)
	out, _ := execCerne(t, "lint", "--dir", projDir)
	if !strings.Contains(out, "PRECONDITION-MALFORMED") || !strings.Contains(out, "RB-0001") {
		t.Fatalf("lint must report the malformed precondition:\n%s", out)
	}
}
