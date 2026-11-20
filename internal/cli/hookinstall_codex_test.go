package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// TestHookInstallCodexSaysHowToTrustAndCheck (F-0474 crit. 3): Codex reads
// .codex/ only in a trusted project, so the output names the trust line and the
// two checks.
func TestHookInstallCodexSaysHowToTrustAndCheck(t *testing.T) {
	dir := t.TempDir()
	if err := project.SaveConfig(dir, project.Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "hook", "install", "--dir", dir, "--provider", "codex")
	if err != nil {
		t.Fatalf("hook install: %v\noutput: %s", err, out)
	}
	for _, want := range []string{
		"only reads .codex/ in a trusted project",
		"[projects.\"" + real + "\"]",
		"replaces your own default profile inside this project",
		"extends :workspace",
		"-s/-P on the command line still win",
		`trust_level = "trusted"`,
		"printenv CERNE_ACTOR",
		"cerne next",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// Other providers do not get the Codex guidance.
	out, err = execCerne(t, "hook", "install", "--dir", dir, "--provider", "gemini")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "trusted project") {
		t.Errorf("codex guidance printed for gemini:\n%s", out)
	}
}

// A relative --dir prints the absolute project path: Codex cannot match ".".
func TestHookInstallCodexTrustHintIsAbsolute(t *testing.T) {
	dir := t.TempDir()
	if err := project.SaveConfig(dir, project.Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	abs, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "hook", "install", "--dir", ".", "--provider", "codex")
	if err != nil {
		t.Fatalf("hook install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "[projects.\""+abs+"\"]") || strings.Contains(out, `[projects."."]`) {
		t.Errorf("want absolute path %q in:\n%s", abs, out)
	}
}

// The hint resolves symlinks (macOS /var -> /private/var): Codex sees the real path.
func TestHookInstallCodexTrustHintResolvesSymlinks(t *testing.T) {
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := project.SaveConfig(real, project.Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable")
	}
	out, err := execCerne(t, "hook", "install", "--dir", link, "--provider", "codex")
	if err != nil {
		t.Fatalf("hook install: %v\n%s", err, out)
	}
	if !strings.Contains(out, "[projects.\""+real+"\"]") || strings.Contains(out, link) {
		t.Errorf("want %q (not the link) in:\n%s", real, out)
	}
}
