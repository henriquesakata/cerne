package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"

	"github.com/henriquesakata/cerne/internal/project"
)

// F-0512 crit. 1 (review #1a): the reviewer's reproduction, replayed. An agent
// can rewrite .cernerc from inside the sandbox; the session-start refresh that
// follows must renew the rules but never write the new backend folder into
// .cursor/sandbox.json, a file Cursor keeps out of the agent's reach.
func TestAutoRefreshNeverWidensTheCursorSandbox(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CERNE_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	backend := filepath.Join(home, "backends", "demo.git")
	if _, err := git.PlainInit(backend, true); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	if err := project.SaveConfig(proj, project.Config{ProjectID: "demo", Backend: backend}); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerne(t, "hook", "install", "--dir", proj, "--provider", "cursor"); err != nil {
		t.Fatalf("hook install: %v\n%s", err, out)
	}
	sandbox := filepath.Join(proj, ".cursor", "sandbox.json")
	before, err := os.ReadFile(sandbox)
	if err != nil {
		t.Fatal(err)
	}

	ssh := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(ssh, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := project.SaveConfig(proj, project.Config{ProjectID: "demo", Backend: ssh}); err != nil {
		t.Fatal(err)
	}
	rules := filepath.Join(proj, ".cursor", "rules", "cerne.mdc")
	if err := os.Remove(rules); err != nil {
		t.Fatal(err)
	}
	out, err := execCerneStdin(t, sessionPayload("s1", proj, "startup"), "hook", "session-context", "--format", "cursor")
	if err != nil {
		t.Fatalf("session-context: %v\n%s", err, out)
	}
	if _, err := os.Stat(rules); err != nil {
		t.Fatalf("the refresh did not run (rules not recreated): %v\n%s", err, out)
	}
	after, _ := os.ReadFile(sandbox)
	if string(after) != string(before) || strings.Contains(string(after), ".ssh") {
		t.Errorf("the refresh rewrote the sandbox:\n%s\n---\n%s", before, after)
	}
}
