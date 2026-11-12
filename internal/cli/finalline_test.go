package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// execCerneSplit runs the CLI with stdout and stderr apart, like a shell
// pipeline `cerne push | tail` where only stdout goes through the pipe.
func execCerneSplit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := Root()
	o, e := &bytes.Buffer{}, &bytes.Buffer{}
	root.SetOut(o)
	root.SetErr(e)
	root.SetArgs(args)
	err = root.Execute()
	return o.String(), e.String(), err
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// F-0876 crit. 4: push and sync end with one stable line on stderr, the last
// thing printed, so it survives `| tail` (which only filters stdout).
func TestPushFinalLineOK(t *testing.T) {
	projDir, _, _, specPath := pulledProject(t)
	mustWrite(t, specPath, realDoc("F-0001", "Edicao que sobe sem conflito."))
	stdout, stderr, err := execCerneSplit(t, "push", "--dir", projDir)
	if err != nil {
		t.Fatalf("push: %v\n%s%s", err, stdout, stderr)
	}
	if got := lastLine(stderr); got != "cerne: resultado=OK" {
		t.Fatalf("last stderr line = %q, want cerne: resultado=OK\nstderr:\n%s", got, stderr)
	}
	// Nothing to send is still a success with the line.
	_, stderr, err = execCerneSplit(t, "push", "--dir", projDir)
	if err != nil || lastLine(stderr) != "cerne: resultado=OK" {
		t.Fatalf("nothing-to-send: err=%v last=%q", err, lastLine(stderr))
	}
}

func TestPushFinalLineFailsOnConflictAndKeepsExit(t *testing.T) {
	projDir, origin, _, specPath := pulledProject(t)
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := gitback.Clone(origin, clone2); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	mustWrite(t, filepath.Join(clone2, "content", "features", "F-0001", "spec.md"), realDoc("F-0001", "Versao deles."))
	if _, err := gitback.CommitAndPush(clone2, "their spec edit"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	mustWrite(t, specPath, realDoc("F-0001", "Versao minha, divergente."))

	_, stderr, err := execCerneSplit(t, "push", "--dir", projDir)
	if err == nil {
		t.Fatalf("conflict must keep the failing exit")
	}
	last := lastLine(stderr)
	if !strings.HasPrefix(last, "cerne: resultado=FALHA ") || !strings.Contains(last, "1 document(s) in conflict") {
		t.Fatalf("last stderr line = %q, want resultado=FALHA with the cause\nstderr:\n%s", last, stderr)
	}
}

func TestSyncFinalLine(t *testing.T) {
	projDir, _, _, _ := pulledProject(t)
	_, stderr, err := execCerneSplit(t, "sync", "--dir", projDir)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := lastLine(stderr); got != "cerne: resultado=OK" {
		t.Fatalf("sync last stderr line = %q, want cerne: resultado=OK", got)
	}
	// A failure before the push (no project here) also ends in FALHA.
	_, stderr, err = execCerneSplit(t, "sync", "--dir", t.TempDir())
	if err == nil || !strings.HasPrefix(lastLine(stderr), "cerne: resultado=FALHA ") {
		t.Fatalf("sync outside a project: err=%v last=%q", err, lastLine(stderr))
	}
}

// F-0876 round 1: errors cobra raises before RunE (unknown flag, stray
// argument) end in the same FALHA line, printed once, and keep the failing exit.
func TestFinalLineOnFlagAndArgErrors(t *testing.T) {
	projDir, _, _, _ := pulledProject(t)
	for name, args := range map[string][]string{
		"push unknown flag": {"push", "--dir", projDir, "--bogus"},
		"push stray arg":    {"push", "--dir", projDir, "extra"},
		"sync unknown flag": {"sync", "--dir", projDir, "--bogus"},
		"sync stray arg":    {"sync", "--dir", projDir, "extra"},
	} {
		t.Run(name, func(t *testing.T) {
			_, stderr, err := execCerneSplit(t, args...)
			if err == nil {
				t.Fatalf("must keep the failing exit")
			}
			if last := lastLine(stderr); !strings.HasPrefix(last, "cerne: resultado=FALHA ") {
				t.Fatalf("last stderr line = %q\nstderr:\n%s", last, stderr)
			}
			if n := strings.Count(stderr, "Error: "); n != 1 {
				t.Fatalf("error printed %d times, want 1:\n%s", n, stderr)
			}
		})
	}
}

// F-0876 round 1: --dry-run ends in the line too, with and without changes.
func TestPushDryRunFinalLine(t *testing.T) {
	projDir, _, _, specPath := pulledProject(t)
	_, stderr, err := execCerneSplit(t, "push", "--dry-run", "--dir", projDir)
	if err != nil || lastLine(stderr) != "cerne: resultado=OK" {
		t.Fatalf("dry-run, nothing changed: err=%v stderr:\n%s", err, stderr)
	}
	mustWrite(t, specPath, realDoc("F-0001", "Edicao so para o dry-run."))
	// The fixture doc is lint-incomplete, so the dry-run fails: the line says so.
	stdout, stderr, err := execCerneSplit(t, "push", "--dry-run", "--dir", projDir)
	if err == nil || !strings.Contains(stdout, "Dry-run:") || !strings.HasPrefix(lastLine(stderr), "cerne: resultado=FALHA ") {
		t.Fatalf("dry-run with a change: err=%v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
}

// F-0876 round 1: a push that conflicts says the phase failed, never done.
func TestPushConflictPhaseSaysFailedNotDone(t *testing.T) {
	projDir, origin, _, specPath := pulledProject(t)
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := gitback.Clone(origin, clone2); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	mustWrite(t, filepath.Join(clone2, "content", "features", "F-0001", "spec.md"), realDoc("F-0001", "Versao deles."))
	if _, err := gitback.CommitAndPush(clone2, "their spec edit"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	mustWrite(t, specPath, realDoc("F-0001", "Versao minha, divergente."))
	_, stderr, _ := execCerneSplit(t, "push", "--dir", projDir)
	if strings.Contains(stderr, "reconciling with the backend: done") ||
		!strings.Contains(stderr, "reconciling with the backend: failed (") {
		t.Fatalf("conflicting reconcile must close as failed:\n%s", stderr)
	}
}

// F-0876 round 1: a project adopted through CERNE_ROOT is said on stderr.
func TestCerneRootAdoptionIsAnnounced(t *testing.T) {
	projDir, _, _, _ := pulledProject(t)
	outside := t.TempDir()
	t.Setenv("CERNE_ROOT", projDir)
	t.Chdir(outside) // F-0354: an explicit --dir never falls back to CERNE_ROOT; the cwd does

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	_, stderr, cerr := execCerneSplit(t, "push")
	os.Stderr = old
	w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if cerr != nil {
		t.Fatalf("push via CERNE_ROOT: %v\n%s", cerr, stderr)
	}
	if want := "using CERNE_ROOT=" + projDir; !strings.Contains(buf.String(), want) {
		t.Fatalf("adoption not announced (want %q):\n%s", want, buf.String())
	}
	// Inside the project the walk finds it: no note.
	r2, w2, _ := os.Pipe()
	os.Stderr = w2
	_, _, _ = execCerneSplit(t, "push", "--dir", projDir)
	os.Stderr = old
	w2.Close()
	var buf2 bytes.Buffer
	_, _ = buf2.ReadFrom(r2)
	if strings.Contains(buf2.String(), "CERNE_ROOT") {
		t.Fatalf("no note expected when the walk finds the project:\n%s", buf2.String())
	}
}
