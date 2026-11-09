package cli

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0873 r1 achado 1: without --passos the list is inferred from the body and
// carries S2/S9 from an unrelated title; ordering is not judged then, or every
// step would come out "out of order" and become a false erratum.
func TestRunbookInferredStepsDoNotInventOutOfOrder(t *testing.T) {
	projDir, _, _ := setupProject(t)
	bodyFile := filepath.Join(t.TempDir(), "rb.md")
	mustWrite(t, bodyFile, rbRunFixtureBody)
	rb(t, projDir, "runbook", "new", "Giro de credenciais", "--file", bodyFile)
	id := strings.Fields(rb(t, projDir, "runbook", "run", "RB-0001", "--janela", "J3"))[0]
	out := rb(t, projDir, "runbook", "step", id, "S3", "--ok", "vai")
	if strings.Contains(out, "out of order") {
		t.Fatalf("inferred list must not flag order:\n%s", out)
	}
	rep := rb(t, projDir, "runbook", "report", id, "--errata")
	if !strings.Contains(rep, "0 errata item(s)") || !strings.Contains(rep, "no deviation") {
		t.Fatalf("no false deviation expected:\n%s", rep)
	}
}

// With an explicit --passos the order is still judged.
func TestRunbookExplicitStepsStillFlagOrder(t *testing.T) {
	projDir, _, id := runRunFixture(t)
	if out := rb(t, projDir, "runbook", "step", id, "S5", "--ok", "vai"); !strings.Contains(out, "out of order") {
		t.Fatalf("explicit list must flag order:\n%s", out)
	}
}

// achado 2: one function refuses e-mail and person ids in every free-text flag.
func TestRunbookRefusesPersonalDataInEveryFreeTextFlag(t *testing.T) {
	projDir, _, id := runRunFixture(t)
	const uuid = "123e4567-e89b-12d3-a456-426614174000"
	step := func(extra ...string) []string {
		return append([]string{"runbook", "step", id, "S3", "--dir", projDir}, extra...)
	}
	cases := map[string][]string{
		"ok email":       step("--ok", "vai fulano@x.com"),
		"ok uuid":        step("--ok", "vai "+uuid),
		"pare email":     step("--pare", "falhou, ver fulano@x.com"),
		"pare uuid":      step("--pare", "user "+uuid),
		"pare cpf":       step("--pare", "cpf 123.456.789-09"),
		"por email":      step("--ok", "vai", "--por", "fulano@x.com"),
		"por handle":     step("--ok", "vai", "--por", "@dono"),
		"por uuid":       step("--ok", "vai", "--por", uuid),
		"esperada email": step("--ok", "vai", "--esperada", "fulano@x.com"),
		"evidencia mail": step("--ok", "vai", "--evidencia", "/Users/fulano@x.com/e.log"),
		"evidencia uuid": step("--ok", "vai", "--evidencia", "/tmp/"+uuid+".log"),
		"passos email":   {"runbook", "run", "RB-0001", "--janela", "J9", "--passos", "S1,fulano@x.com", "--dir", projDir},
		"passos junk":    {"runbook", "run", "RB-0001", "--janela", "J9", "--passos", "S1,joao", "--dir", projDir},
		"palavra email":  {"runbook", "run", "RB-0001", "--janela", "J9", "--passos", "S1", "--palavra", "fulano@x.com", "--dir", projDir},
		"palavra uuid":   {"runbook", "run", "RB-0001", "--janela", "J9", "--passos", "S1", "--palavra", uuid, "--dir", projDir},
		"janela email":   {"runbook", "run", "RB-0001", "--janela", "fulano@x.com", "--passos", "S1", "--dir", projDir},
	}
	for name, args := range cases {
		if out, err := execCerne(t, args...); err == nil {
			t.Errorf("%s must be refused:\n%s", name, out)
		}
	}
	// A digest after @ in a log is not a person: it stays allowed in --pare.
	rb(t, projDir, "runbook", "step", id, "S3", "--pare", "pull repo@sha256:abcdef falhou")
}

// achado 3 (M4): a FIFO is refused before any open/hash; it would block forever.
func TestHashEvidenceRefusesNonRegularWithoutOpening(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "p")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("mkfifo:", err)
	}
	done := make(chan error, 1)
	go func() { _, err := app.HashEvidence(fifo); done <- err }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("fifo err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("HashEvidence blocked on a FIFO")
	}
	if _, err := app.HashEvidence(t.TempDir()); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("dir err = %v", err)
	}
}

// achado 5: only an active runbook can be run.
func TestRunbookRunRefusesInactiveRunbook(t *testing.T) {
	projDir, _ := runbookFixture(t)
	rb(t, projDir, "runbook", "retire", "RB-0001", "--reason", "obsoleto")
	out, err := execCerne(t, "runbook", "run", "RB-0001", "--janela", "J1", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "superseded") {
		t.Fatalf("superseded runbook must be refused (err %v):\n%s", err, out)
	}
	if _, err := execCerne(t, "runbook", "run", "RB-0999", "--janela", "J1", "--dir", projDir); err == nil {
		t.Fatal("unknown runbook must be refused")
	}
}

// achado 5: a step ok twice is appended (append-only), stays clean and counts
// once in the report; --pare is recorded as a deviation.
func TestRunbookRepeatedStepAndPare(t *testing.T) {
	projDir, repoDir, id := runRunFixture(t, "--palavra", "vai")
	rb(t, projDir, "runbook", "step", id, "S3", "--ok", "vai")
	rb(t, projDir, "runbook", "step", id, "S3", "--ok", "vai")
	run, _ := store.LoadRun(repoDir, id)
	if len(run.Entries) != 2 || run.Entries[1].OutOfOrder || run.Entries[1].Seq != 2 {
		t.Fatalf("entries = %+v", run.Entries)
	}
	rb(t, projDir, "runbook", "step", id, "S4", "--pare", "alerta firing", "--por", "dono")
	rep := rb(t, projDir, "runbook", "report", id)
	if !strings.Contains(rep, "1 deviation(s)") || !strings.Contains(rep, "PARE") {
		t.Fatalf("report:\n%s", rep)
	}
}

// The step letter stays lowercase (RB-0070 has S3a, S4a-S4g): only the leading
// S is normalised, so --passos and `step` match the id the body uses.
func TestRunbookStepLetterStaysLowercase(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	bodyFile := filepath.Join(t.TempDir(), "rb.md")
	mustWrite(t, bodyFile, "# R\n\n- S3 a\n- S3a b\n- S4b c\n")
	rb(t, projDir, "runbook", "new", "R", "--file", bodyFile)
	id := strings.Fields(rb(t, projDir, "runbook", "run", "RB-0001", "--janela", "J3", "--passos", "s3,S3a,s4b"))[0]
	run, err := store.LoadRun(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(run.Steps, " ") != "S3 S3a S4b" {
		t.Fatalf("steps = %v", run.Steps)
	}
	rb(t, projDir, "runbook", "step", id, "S3", "--ok", "vai")
	rb(t, projDir, "runbook", "step", id, "s3A", "--ok", "vai")
	rb(t, projDir, "runbook", "step", id, "S4b", "--ok", "vai")
	if rep := rb(t, projDir, "runbook", "report", id); !strings.Contains(rep, "no deviation") {
		t.Fatalf("report:\n%s", rep)
	}
}
