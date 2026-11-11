package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0873 r2 achado 1 (N5): the e-mail check on --evidencia is exercised by a
// file that EXISTS, so os.Stat cannot be what refuses it.
func TestHashEvidenceRealFileWithEmailInNameIsRefused(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "fulano@x.com.log")
	mustWrite(t, bad, "log")
	if _, err := app.HashEvidence(bad); err == nil || !strings.Contains(err.Error(), "e-mail") {
		t.Fatalf("real file with e-mail in the name must be refused for it: %v", err)
	}
	// r2 achado 3: a UUID in a path is legitimate (pod dirs, dumps).
	pod := filepath.Join(dir, "pods", "123e4567-e89b-12d3-a456-426614174000")
	if err := os.MkdirAll(pod, 0o755); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(pod, "app.log")
	mustWrite(t, good, "log")
	ev, err := app.HashEvidence(good)
	if err != nil || ev.Path != good || ev.SHA256 == "" {
		t.Fatalf("uuid in a path must be accepted: %+v %v", ev, err)
	}
}

// achado 2: a version after @ is not an e-mail; a real e-mail still is.
func TestRunbookVersionsAreNotEmailsButEmailsStillAre(t *testing.T) {
	projDir, _, id := runRunFixture(t)
	okFile := filepath.Join(t.TempDir(), "build@2.0.log")
	mustWrite(t, okFile, "log")
	rb(t, projDir, "runbook", "step", id, "S3", "--ok", "instalou next@15.0.1 e pkg@1.2.3", "--evidencia", okFile)
	for _, bad := range []string{"fulano@x.com", "a.b+c@sub.example.com.br", "ze@1password.com", "x@y.co"} {
		if out, err := execCerne(t, "runbook", "step", id, "S4", "--ok", "vai "+bad, "--dir", projDir); err == nil {
			t.Errorf("e-mail %q must still be refused:\n%s", bad, out)
		}
	}
	// the version case, on --pare: this records the stop, it is not refused.
	rb(t, projDir, "runbook", "step", id, "S4", "--pare", "falhou ao instalar next@15.0.1")
}

// achado 3, declared limit: a registration number has no format that tells it
// from a ticket or a pid, so it is not refused (documented in refusePersonal).
func TestRunbookRegistrationNumberIsADeclaredLimit(t *testing.T) {
	projDir, _, id := runRunFixture(t)
	rb(t, projDir, "runbook", "step", id, "S3", "--pare", "matricula 12345")
}

// achado 4: a run written before steps_explicit existed carries no mark, so it
// is treated as inferred: no false "out of order".
func TestLegacyRunWithoutExplicitMarkDoesNotJudgeOrder(t *testing.T) {
	projDir, repoDir, id := runRunFixture(t)
	path := filepath.Join(repoDir, "content", "runs", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"steps_explicit": true`) {
		t.Fatalf("an explicit --passos must be recorded as such:\n%s", data)
	}
	legacy := strings.NewReplacer(`"steps_explicit": true,`, "", `"state": "aberta",`, "").Replace(string(data))
	if legacy == string(data) {
		t.Fatalf("fixture not rewritten:\n%s", data)
	}
	mustWrite(t, path, legacy)
	// the backend resets to HEAD before each mutation: commit and push the old-format file
	for _, g := range [][]string{{"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t.test", "commit", "-qm", "legacy run"}, {"push", "-q", "origin", "HEAD"}} {
		if out, err := exec.Command("/usr/bin/git", append([]string{"-C", repoDir}, g...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", g, err, out)
		}
	}
	if out := rb(t, projDir, "runbook", "step", id, "S6", "--ok", "vai"); strings.Contains(out, "out of order") {
		t.Fatalf("legacy run must not judge order:\n%s", out)
	}
}

// achado 5: a repeated step in --passos is refused, naming it.
func TestRunbookRunRefusesDuplicatedSteps(t *testing.T) {
	projDir, _ := runbookFixture(t)
	out, err := execCerne(t, "runbook", "run", "RB-0001", "--janela", "J1", "--passos", "S3,S4,S3", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "S3 is listed twice") {
		t.Fatalf("duplicate step must be refused (err %v):\n%s", err, out)
	}
}

// achado 6: the run has its own life cycle, aberta -> parada -> concluida, and
// each refused transition fails with an explicit error.
func TestRunLifecycleStates(t *testing.T) {
	projDir, repoDir, id := runRunFixture(t)
	state := func() string {
		t.Helper()
		r, err := store.LoadRun(repoDir, id)
		if err != nil {
			t.Fatal(err)
		}
		return r.Status()
	}
	if s := state(); s != store.RunOpen {
		t.Fatalf("new run = %q", s)
	}
	rb(t, projDir, "runbook", "step", id, "S3", "--ok", "vai")
	if s := state(); s != store.RunOpen {
		t.Fatalf("after an ok = %q", s)
	}
	if out := rb(t, projDir, "show", id); !strings.Contains(out, "state: aberta") {
		t.Fatalf("show must print the state:\n%s", out)
	}
	rb(t, projDir, "runbook", "step", id, "S4", "--pare", "alerta firing")
	if s := state(); s != store.RunStopped {
		t.Fatalf("after PARE = %q", s)
	}
	// a step after PARE is refused, ok or pare
	for _, a := range [][]string{{"--ok", "vai"}, {"--pare", "de novo"}} {
		args := append([]string{"runbook", "step", id, "S5", "--dir", projDir}, a...)
		if out, err := execCerne(t, args...); err == nil || !strings.Contains(err.Error(), "parada") {
			t.Fatalf("step after PARE must be refused as parada (err %v):\n%s", err, out)
		}
	}
	rb(t, projDir, "work", "done", id)
	if s := state(); s != store.RunClosed {
		t.Fatalf("after work done = %q", s)
	}
	if out, err := execCerne(t, "runbook", "step", id, "S5", "--ok", "vai", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "concluida") {
		t.Fatalf("step on a closed run must be refused (err %v):\n%s", err, out)
	}
	// closing twice is refused, and leaves the run closed
	if out, err := execCerne(t, "work", "done", id, "--dir", projDir); err == nil {
		t.Fatalf("closing twice must be refused:\n%s", out)
	}
	if s := state(); s != store.RunClosed {
		t.Fatalf("after a refused second close = %q", s)
	}
}

// abandoning the work concludes the run too, and an open run closes without a
// PARE.
func TestRunLifecycleAbandonConcludes(t *testing.T) {
	projDir, repoDir, id := runRunFixture(t)
	rb(t, projDir, "runbook", "step", id, "S3", "--ok", "vai")
	rb(t, projDir, "work", "abandon", id, "--reason", "janela cancelada")
	r, err := store.LoadRun(repoDir, id)
	if err != nil || r.Status() != store.RunClosed {
		t.Fatalf("abandoned run = %q (%v)", r.Status(), err)
	}
	if out, err := execCerne(t, "runbook", "step", id, "S4", "--ok", "vai", "--dir", projDir); err == nil {
		t.Fatalf("step on an abandoned run must be refused:\n%s", out)
	}
}
