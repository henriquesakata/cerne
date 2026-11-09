package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
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
