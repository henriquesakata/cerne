package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
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
	for _, k := range demoSentinelVars {
		dir := filepath.Join(home, k)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv(k, dir)
	}
	t.Chdir(t.TempDir())
}

// demoSentinelVars point at empty sentinel dirs during a test: the demo must
// write nothing through them and give them back unchanged.
var demoSentinelVars = []string{"CERNE_CACHE_DIR", "CERNE_DATA_DIR", "CERNE_CONFIG_DIR", "CERNE_ROOT"}

func assertSentinelsUntouched(t *testing.T) {
	t.Helper()
	home := os.Getenv("HOME")
	for _, k := range demoSentinelVars {
		dir := filepath.Join(home, k)
		if got := os.Getenv(k); got != dir {
			t.Errorf("%s = %q after the demo, want it restored to %q", k, got, dir)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("the demo wrote into %s (%s): %v", k, dir, entries)
		}
	}
}

// failAt makes the step whose first two args are verb fail (or panic).
func failAt(t *testing.T, verb string, act func()) {
	t.Helper()
	t.Cleanup(func() { demoRun = nil })
	demoRun = func(body string, args ...string) (string, error) {
		if len(args) > 1 && args[0]+" "+args[1] == verb {
			act()
			return "boom\n", errors.New("exit 1")
		}
		return cerneInProcess(body, args...)
	}
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
	assertSentinelsUntouched(t)
}

// F-0412: the README quickstart runs `cerne demo`, then a first command in the
// project from the user's own shell, whose cache dir is not the demo's. That
// command clones the cache and says so as progress, never as a warning.
func TestDemo_FirstCommandPrintsNoWarning(t *testing.T) {
	demoEnv(t)
	if out, err := runDemoCmd(t, "cerne-demo"); err != nil {
		t.Fatalf("cerne demo: %v\n%s", err, out)
	}
	assertSentinelsUntouched(t)
	abs, _ := filepath.Abs("cerne-demo")
	t.Setenv("CERNE_ROOT", "") // the user's shell has no CERNE_ROOT
	t.Chdir(filepath.Join(abs, "taskflow"))

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	out, berr := execCerne(t, "brief")
	os.Stderr = old
	w.Close()
	var stderr bytes.Buffer
	_, _ = stderr.ReadFrom(r)
	if berr != nil {
		t.Fatalf("cerne brief: %v\n%s", berr, out)
	}
	want := "cerne: cloning the backend from " + filepath.Join(abs, "taskflow-docs.git") + " into the local cache...\n"
	if stderr.String() != want {
		t.Errorf("stderr = %q, want only %q", stderr.String(), want)
	}
	if !strings.Contains(out, "F-0001") {
		t.Errorf("brief printed no features:\n%s", out)
	}
}

// A step failing after the gates were opened removes the whole demo and names
// the step.
func TestDemo_FailureRemovesTheDemo(t *testing.T) {
	demoEnv(t)
	failAt(t, "feature reject", func() {})
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
	assertSentinelsUntouched(t)
}

// Only what the command created goes: a parent that existed before stays.
func TestDemo_FailureRemovesOnlyWhatItCreated(t *testing.T) {
	demoEnv(t)
	failAt(t, "feature reject", func() {})
	if err := os.Mkdir("a", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("a", "keep.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runDemoCmd(t, filepath.Join("a", "b", "c")); err == nil {
		t.Fatal("cerne demo succeeded although a step failed")
	}
	if entries, _ := os.ReadDir("a"); len(entries) != 1 || entries[0].Name() != "keep.txt" {
		t.Errorf("a/ = %v, want only keep.txt (a/b was created by the command)", entries)
	}
	if _, err := runDemoCmd(t, filepath.Join("x", "y", "z")); err == nil {
		t.Fatal("cerne demo succeeded although a step failed")
	}
	if _, err := os.Stat("x"); !os.IsNotExist(err) {
		t.Error("x/ was created by the command and left behind")
	}
}

// A panic in a step still removes the demo.
func TestDemo_PanicRemovesTheDemo(t *testing.T) {
	demoEnv(t)
	failAt(t, "feature reject", func() { panic("step blew up") })
	func() {
		defer func() { _ = recover() }()
		_, _ = runDemoCmd(t, "demo")
	}()
	if _, err := os.Stat("demo"); !os.IsNotExist(err) {
		t.Error("a panic left a half-built demo behind")
	}
}

// When the removal itself fails, the error says so instead of claiming it.
func TestDemo_FailedRemovalIsReported(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	demoEnv(t)
	locked := ""
	failAt(t, "feature reject", func() {
		wd, _ := os.Getwd()
		locked = filepath.Join(wd, "locked")
		_ = os.Mkdir(locked, 0o755)
		_ = os.WriteFile(filepath.Join(locked, "f"), nil, 0o644)
		_ = os.Chmod(locked, 0o500)
	})
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	_, err := runDemoCmd(t, "demo")
	if err == nil || !strings.Contains(err.Error(), "could not remove") || strings.Contains(err.Error(), "; removed") {
		t.Errorf("err = %v, want it to report the failed removal", err)
	}
}

// An interrupt stops the build between steps; it never moves the process cwd
// while a step runs, so neither the caller's cwd nor a sibling project is
// touched.
func TestDemo_InterruptLeavesCallerUntouched(t *testing.T) {
	demoEnv(t)
	caller, _ := os.Getwd()
	if err := os.WriteFile("mine.txt", []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	sibling := filepath.Join(base, "sibling")
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, ".cernerc"), []byte("project_id = \"sib\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { demoRun = nil })
	sent := false
	demoRun = func(body string, args ...string) (string, error) {
		out, err := cerneInProcess(body, args...)
		if !sent && args[0] == "feature" && args[1] == "approve" {
			sent = true
			p, _ := os.FindProcess(os.Getpid())
			if p.Signal(os.Interrupt) != nil {
				t.Skip("cannot signal this process")
			}
			for i := 0; i < 200 && !demoCancelled.Load(); i++ {
				time.Sleep(10 * time.Millisecond)
			}
		}
		return out, err
	}
	_, err := runDemoCmd(t, filepath.Join(base, "demo"))
	if !errors.Is(err, errDemoInterrupted) {
		t.Fatalf("err = %v, want errDemoInterrupted", err)
	}
	if wd, _ := os.Getwd(); wd != caller {
		t.Errorf("cwd = %s, want the caller's %s", wd, caller)
	}
	if entries, _ := os.ReadDir(caller); len(entries) != 1 {
		t.Errorf("the caller's cwd changed: %v", entries)
	}
	if entries, _ := os.ReadDir(sibling); len(entries) != 1 {
		t.Errorf("the sibling project changed: %v", entries)
	}
	if _, err := os.Stat(filepath.Join(base, "demo")); !os.IsNotExist(err) {
		t.Error("the interrupted demo was left behind")
	}
	assertSentinelsUntouched(t)
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
	// A symlinked parent that leads into a project is inside it, too.
	inner := filepath.Join(".", "inner")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	abs, _ := filepath.Abs(inner)
	if err := os.Symlink(abs, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, err := runDemoCmd(t, filepath.Join(link, "demo")); !errors.Is(err, errDemoInsideProject) {
		t.Errorf("symlinked parent into a project: err = %v, want errDemoInsideProject", err)
	}
}
