//go:build !windows

package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// Black-box tests of examples/demo/seed.sh (F-0358): the script runs for real,
// against a cerne binary built from this tree, and the project it generates
// is inspected through the public CLI.

var demo struct {
	once sync.Once
	bin  string // directory holding the built cerne
	dir  string // the seeded <dir>
	out  string
	err  error
}

// demoBin builds cerne once and returns the directory to put on PATH.
func demoBin(t *testing.T) string {
	t.Helper()
	demo.once.Do(func() {
		root, err := os.MkdirTemp("", "cerne-demo-")
		if err != nil {
			demo.err = err
			return
		}
		demo.bin = filepath.Join(root, "bin")
		if out, err := exec.Command("go", "build", "-o", filepath.Join(demo.bin, "cerne"), "../cmd/cerne").CombinedOutput(); err != nil {
			demo.err, demo.out = err, string(out)
			return
		}
		demo.dir = filepath.Join(root, "demo")
		demo.out, demo.err = runSeed(demo.bin, root, demo.dir)
	})
	if demo.err != nil {
		t.Fatalf("building cerne or seeding the demo failed: %v\n%s", demo.err, demo.out)
	}
	return demo.bin
}

// runSeed runs the seed script as an agent session would: CERNE_ACTOR=agent
// is what `cerne hook install` stamps, and the script must still finish.
func runSeed(bin, home, dest string, extraEnv ...string) (string, error) {
	script, err := filepath.Abs("../examples/demo/seed.sh")
	if err != nil {
		return "", err
	}
	cmd := exec.Command("sh", script, dest)
	cmd.Env = append([]string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + home, "CERNE_ACTOR=agent"}, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// demoCerne runs the built cerne inside the generated project.
func demoCerne(t *testing.T, args ...string) (string, error) {
	t.Helper()
	bin := demoBin(t)
	cmd := exec.Command(filepath.Join(bin, "cerne"), args...)
	cmd.Dir = filepath.Join(demo.dir, "taskflow")
	cmd.Env = []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + filepath.Dir(bin), "CERNE_CACHE_DIR=" + filepath.Join(demo.dir, ".cache")}
	out, err := cmd.Output()
	return string(out), err
}

func demoJSON(t *testing.T, into any, args ...string) {
	t.Helper()
	out, err := demoCerne(t, args...)
	if err != nil {
		t.Fatalf("cerne %v: %v\n%s", args, err, out)
	}
	if err := json.Unmarshal([]byte(out), into); err != nil {
		t.Fatalf("cerne %v: not JSON: %v\n%s", args, err, out)
	}
}

func TestDemoSeed_BuildsProject(t *testing.T) {
	demoBin(t)
	for _, p := range []string{"taskflow/.cernerc", "taskflow/docs.lock", "taskflow/.docs/CONTEXT.md", "taskflow-docs.git/HEAD"} {
		if _, err := os.Stat(filepath.Join(demo.dir, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	// The gates were opened only while seeding.
	rc, err := os.ReadFile(filepath.Join(demo.dir, "taskflow", ".cernerc"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rc), "[gates]") {
		t.Errorf("the generated .cernerc still opens the gates:\n%s", rc)
	}
}

func TestDemoSeed_CoversEveryState(t *testing.T) {
	var list struct {
		Features []struct{ ID, State string }
	}
	demoJSON(t, &list, "feature", "list", "--all", "--json")
	have := map[string]bool{}
	for _, f := range list.Features {
		have[f.State] = true
	}
	for _, state := range []string{"candidate", "approved", "in_development", "implemented", "done", "rejected"} {
		if !have[state] {
			t.Errorf("no feature in state %q", state)
		}
	}
}

func TestDemoSeed_HasEveryEntityKind(t *testing.T) {
	var list struct {
		Features []struct{ ID, Parent string }
	}
	demoJSON(t, &list, "feature", "list", "--all", "--json")
	children := 0
	for _, f := range list.Features {
		if f.Parent != "" {
			children++
		}
	}
	if children < 2 {
		t.Errorf("the epic has %d child feature(s), want at least 2", children)
	}

	var decisions struct {
		Decisions []struct {
			State        string
			SupersededBy string `json:"superseded_by"`
		}
	}
	demoJSON(t, &decisions, "decision", "list", "--all", "--json")
	active, superseded := 0, 0
	for _, d := range decisions.Decisions {
		switch {
		case d.State == "active":
			active++
		case d.SupersededBy != "":
			superseded++
		}
	}
	if active == 0 || superseded == 0 {
		t.Errorf("decisions: %d active, %d superseded by another; want at least one of each", active, superseded)
	}

	var brief map[string][]json.RawMessage
	demoJSON(t, &brief, "brief", "--json")
	for _, kind := range []string{"notes", "runbooks", "references"} {
		if len(brief[kind]) == 0 {
			t.Errorf("no %s in the demo", kind)
		}
	}
	var refs struct {
		References []struct {
			External bool
			Informs  []string
		}
	}
	demoJSON(t, &refs, "reference", "list", "--json")
	if len(refs.References) == 0 || !refs.References[0].External || len(refs.References[0].Informs) == 0 {
		t.Errorf("the reference is not an external source linked to a feature: %+v", refs.References)
	}
}

func TestDemoSeed_LintClean(t *testing.T) {
	if out, err := demoCerne(t, "lint"); err != nil {
		t.Errorf("cerne lint on the demo: %v\n%s", err, out)
	}
}

// The demo is what an English-speaking visitor reads: no Portuguese in its
// content, and none of cerne's Portuguese protocol words.
func TestDemoSeed_ContentIsEnglish(t *testing.T) {
	portuguese := regexp.MustCompile(`[áàâãéêíóôõúçÁÀÂÃÉÊÍÓÔÕÚÇ]|\b(?:Barra|Rodada|ESCLARECER|COMO USAR|critério)\b`)
	files := 0
	err := filepath.WalkDir("../examples/demo", func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if m := portuguese.FindString(line); m != "" {
				t.Errorf("%s:%d: %q in %q", path, i+1, m, line)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 20 {
		t.Errorf("walked %d files under examples/demo, expected the whole content tree", files)
	}
}

func TestDemoSeed_RefusesExistingDir(t *testing.T) {
	bin := demoBin(t)
	dest := t.TempDir() // exists
	keep := filepath.Join(dest, "keep.txt")
	if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runSeed(bin, t.TempDir(), dest)
	if err == nil {
		t.Fatalf("the seed ran over an existing directory:\n%s", out)
	}
	entries, _ := os.ReadDir(dest)
	if data, _ := os.ReadFile(keep); string(data) != "mine" || len(entries) != 1 {
		t.Errorf("the existing directory was touched: %v", entries)
	}
}

func TestDemoSeed_NeedsCerne(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "demo")
	script, err := filepath.Abs("../examples/demo/seed.sh")
	if err != nil {
		t.Fatal(err)
	}
	// A PATH with the system tools but no cerne.
	tools := t.TempDir()
	for _, tool := range []string{"sh", "git", "dirname", "mkdir", "cat", "grep", "head", "ls", "printf", "rm", "cp", "mv"} {
		if p, err := exec.LookPath(tool); err == nil {
			_ = os.Symlink(p, filepath.Join(tools, tool))
		}
	}
	cmd := exec.Command("sh", script, dest)
	cmd.Env = []string{"PATH=" + tools, "HOME=" + t.TempDir()}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the seed ran without cerne:\n%s", out)
	}
	if !strings.Contains(string(out), "install.sh") {
		t.Errorf("the refusal does not point at the installer:\n%s", out)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Errorf("the destination was created although the seed refused")
	}
}
