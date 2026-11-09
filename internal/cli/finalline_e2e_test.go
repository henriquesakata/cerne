package cli

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
)

// F-0876 round 1, e2e: the real binary under a real shell pipe. `cerne push |
// tail` has the exit of tail, so what protects the agent is the last stderr
// line: it must come out through the pipeline, and the process exit stays real.
func TestPushFinalLineThroughRealPipe(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "cerne")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cmd/cerne").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	projDir, origin, _, specPath := pulledProject(t)
	clone2 := filepath.Join(t.TempDir(), "clone2")
	if err := gitback.Clone(origin, clone2); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(clone2, "content", "features", "F-0001", "spec.md"), realDoc("F-0001", "Versao deles."))
	if _, err := gitback.CommitAndPush(clone2, "their spec edit"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, specPath, realDoc("F-0001", "Versao minha, divergente."))

	run := func(script string) (string, error) {
		out, err := exec.Command("sh", "-c", script, "sh", bin, projDir).CombinedOutput()
		return strings.TrimRight(string(out), "\n"), err
	}
	// stdout goes through tail; stderr reaches the capture directly, so the
	// line is visible even though tail swallowed the rest of stdout.
	out, _ := run(`"$1" push --dir "$2" | tail -n 1`)
	if !strings.Contains(out, "cerne: resultado=FALHA 1 document(s) in conflict") {
		t.Fatalf("`push | tail` hid the outcome:\n%s", out)
	}
	// Folded into the pipe: still the last line, and the pipe's exit is tail's.
	out, err := run(`"$1" push --dir "$2" 2>&1 | tail -n 1`)
	if err != nil || !strings.HasPrefix(out, "cerne: resultado=FALHA ") {
		t.Fatalf("`push 2>&1 | tail -n 1` = %q err=%v", out, err)
	}
	// Without a pipe the process itself exits non-zero.
	if _, err := run(`"$1" push --dir "$2"`); err == nil {
		t.Fatalf("a conflicting push must exit non-zero")
	}
}
