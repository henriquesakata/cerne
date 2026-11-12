package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// demoTools is a PATH dir holding git and nothing else — no sh (criterion 6).
// One per process: gitback resolves the git binary once, so a per-test dir
// would be gone by the second test.
var demoTools = sync.OnceValue(func() string {
	git, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	dir, err := os.MkdirTemp("", "cerne-demo-tools-")
	if err != nil || os.Symlink(git, filepath.Join(dir, "git")) != nil {
		return ""
	}
	return dir
})

// demoEnv isolates a demo run: its own HOME and cerne dirs, PATH = demoTools.
func demoEnv(t *testing.T) {
	t.Helper()
	tools := demoTools()
	if tools == "" {
		t.Skip("git not on PATH")
	}
	home := t.TempDir()
	t.Setenv("PATH", tools)
	t.Setenv("HOME", home)
	for _, k := range []string{"CERNE_CACHE_DIR", "CERNE_DATA_DIR", "CERNE_CONFIG_DIR"} {
		t.Setenv(k, filepath.Join(home, k))
	}
	t.Chdir(t.TempDir())
}

func runDemoCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := Root()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"demo"}, args...))
	err := root.Execute()
	return out.String(), err
}

func TestDemo_BuildsWithoutShell(t *testing.T) {
	demoEnv(t)
	if _, err := exec.LookPath("sh"); err == nil {
		t.Fatal("sh is still on PATH")
	}
	out, err := runDemoCmd(t)
	if err != nil {
		t.Fatalf("cerne demo: %v\n%s", err, out)
	}
	abs, _ := filepath.Abs("cerne-demo")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if want := `cd "` + filepath.Join(abs, "taskflow") + `" && cerne ui`; lines[len(lines)-1] != want {
		t.Errorf("last line = %q, want %q", lines[len(lines)-1], want)
	}
	if _, err := os.Stat(filepath.Join("cerne-demo", "taskflow", ".docs", "CONTEXT.md")); err != nil {
		t.Errorf("the demo was not materialized: %v", err)
	}
}

// A step failing after the gates were opened removes the whole demo and names
// the step.
func TestDemo_FailureRemovesTheDemo(t *testing.T) {
	demoEnv(t)
	t.Cleanup(func() { demoRun = nil })
	demoRun = func(body string, args ...string) (string, error) {
		if args[0] == "feature" && args[1] == "reject" {
			return "boom\n", errors.New("exit 1")
		}
		return cerneInProcess(body, args...)
	}
	out, err := runDemoCmd(t, "my demo")
	if err == nil {
		t.Fatalf("cerne demo succeeded although a step failed:\n%s", out)
	}
	if !strings.Contains(err.Error(), "cerne feature reject") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("the error does not name the step and its output: %v", err)
	}
	if _, statErr := os.Stat("my demo"); !os.IsNotExist(statErr) {
		t.Error("a half-built demo was left behind")
	}
}

func TestDemo_Refusals(t *testing.T) {
	demoEnv(t)
	if err := os.Mkdir("taken", 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := runDemoCmd(t, "taken"); !errors.Is(err, errDemoExists) {
		t.Errorf("existing dir: err = %v, want errDemoExists", err)
	}
	if err := os.WriteFile(".cernerc", []byte("project_id = \"outer\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runDemoCmd(t, filepath.Join("sub", "demo")); !errors.Is(err, errDemoInsideProject) {
		t.Errorf("inside a project: err = %v, want errDemoInsideProject", err)
	}
	if _, err := os.Stat("sub"); !os.IsNotExist(err) {
		t.Error("a refusal created something")
	}
}
