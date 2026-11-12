package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// TestHookInstallAutoDetectsProviders (F-0174 crit. 2): a project with a
// .cursor/ dir gets the cursor adapter alongside the claude install, with
// degradations in the report.
func TestHookInstallAutoDetectsProviders(t *testing.T) {
	dir := t.TempDir()
	if err := project.SaveConfig(dir, project.Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil { // F-0354: --dir must name a project
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "hook", "install", "--dir", dir)
	if err != nil {
		t.Fatalf("hook install: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "hooks", "cerne-work-gate.sh")); err != nil {
		t.Fatalf("claude install missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cursor", "rules", "cerne.mdc")); err != nil {
		t.Fatalf("cursor adapter missing: %v", err)
	}
	if !strings.Contains(out, "skipped") || !strings.Contains(out, "cursor: work-gate") {
		t.Fatalf("degradations not reported:\n%s", out)
	}
	// Codex not detected, not installed.
	if _, err := os.Stat(filepath.Join(dir, ".codex")); !os.IsNotExist(err) {
		t.Fatal("codex installed without detection")
	}
}

// TestHookInstallProviderFlag (F-0174 crit. 3): --provider forces an install
// even without the config dir, and skips the default claude install.
func TestHookInstallProviderFlag(t *testing.T) {
	dir := t.TempDir()
	if err := project.SaveConfig(dir, project.Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil { // F-0354: --dir must name a project
		t.Fatal(err)
	}
	out, err := execCerne(t, "hook", "install", "--dir", dir, "--provider", "codex")
	if err != nil {
		t.Fatalf("hook install: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".codex", "hooks.json")); err != nil {
		t.Fatalf("codex adapter missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
		t.Fatal("claude installed despite explicit --provider codex")
	}

	if _, err := execCerne(t, "hook", "install", "--dir", dir, "--provider", "emacs"); err == nil {
		t.Fatal("unknown provider must fail")
	}
}

// TestHookInstallWarnsWhenScriptsReplaced (F-0360 crit. 6): one trailing line
// says local edits are not kept, only when an existing script was rewritten.
func TestHookInstallWarnsWhenScriptsReplaced(t *testing.T) {
	const warn = "cerne hook scripts replaced: local edits are not kept — to turn the hooks off for a process, set CERNE_NO_AGENT_HOOKS=1"
	dir := t.TempDir()
	if err := project.SaveConfig(dir, project.Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil {
		t.Fatal(err)
	}
	install := func() string {
		t.Helper()
		out, err := execCerne(t, "hook", "install", "--dir", dir, "--provider", "claude")
		if err != nil {
			t.Fatalf("hook install: %v\noutput: %s", err, out)
		}
		return out
	}
	if out := install(); strings.Contains(out, warn) {
		t.Fatalf("warned on a fresh install:\n%s", out)
	}
	script := filepath.Join(dir, ".claude", "hooks", "cerne-work-gate.sh")
	body, _ := os.ReadFile(script)
	if err := os.WriteFile(script, append([]byte("[ -n \"${ROCKT_EVAL:-}\" ] && exit 0\n"), body...), 0o755); err != nil {
		t.Fatal(err)
	}
	out := install()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[len(lines)-1] != warn || strings.Count(out, warn) != 1 {
		t.Fatalf("want the warning once, as the last line:\n%s", out)
	}
	if out := install(); strings.Contains(out, warn) {
		t.Fatalf("warned with nothing rewritten:\n%s", out)
	}
}
