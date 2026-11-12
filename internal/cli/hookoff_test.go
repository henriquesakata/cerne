package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
)

// runtimeHookSubcommands are the `cerne hook` subcommands an agent host runs
// on its own events — the ones CERNE_NO_AGENT_HOOKS silences (F-0360).
var runtimeHookSubcommands = []string{
	"bind-work", "work-gate", "lifecycle-reminder", "stop-check",
	"provenance", "session-context", "channel", "envelope",
}

// treeSnapshot maps every file under the roots to its size and mtime, so a
// test can tell whether a command wrote anything.
func treeSnapshot(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			snap[p] = fmt.Sprintf("%d %d", info.Size(), info.ModTime().UnixNano())
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return snap
}

// TestRuntimeHooksSilentWhenOff (F-0360 crit. 4): with CERNE_NO_AGENT_HOOKS
// set, each runtime subcommand exits 0 with no output and no write — even on
// the payloads that would otherwise bind a work or deny an edit.
func TestRuntimeHooksSilentWhenOff(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	enableWorkGate(t, projDir)
	t.Setenv("CERNE_NO_AGENT_HOOKS", "1")

	bind := hookJSON(t, "sess-off", projDir, "Bash",
		map[string]any{"command": `cerne work start "Investigar erro X"`},
		map[string]any{"stdout": "W-0001 criada (active) — Investigar erro X (ator: agent)"})
	edit := hookJSON(t, "sess-off", projDir, "Edit",
		map[string]any{"file_path": filepath.Join(projDir, "internal", "a.go")}, nil)

	before := treeSnapshot(t, projDir, os.Getenv("CERNE_CACHE_DIR"))
	for _, sub := range runtimeHookSubcommands {
		stdin := bind
		if sub == "work-gate" {
			stdin = edit
		}
		out, err := execCerneStdin(t, stdin, "hook", sub)
		if err != nil || out != "" {
			t.Errorf("hook %s with the switch on: err %v, output %q", sub, err, out)
		}
	}
	after := treeSnapshot(t, projDir, os.Getenv("CERNE_CACHE_DIR"))
	for p, v := range after {
		if before[p] != v {
			t.Errorf("written with the switch on: %s", p)
		}
	}
	if _, ok, _ := app.CurrentBinding(filepath.Dir(repoDir), repoDir, "sess-off"); ok {
		t.Error("bind-work bound the session with the switch on")
	}
}

// TestRuntimeHooksIgnoreEmptySwitch (F-0360 crit. 3 at the CLI): an empty
// CERNE_NO_AGENT_HOOKS leaves the gate on.
func TestRuntimeHooksIgnoreEmptySwitch(t *testing.T) {
	projDir, _, _ := setupProject(t)
	enableWorkGate(t, projDir)
	t.Setenv("CERNE_NO_AGENT_HOOKS", "")
	stdin := hookJSON(t, "sess-empty", projDir, "Edit",
		map[string]any{"file_path": filepath.Join(projDir, "internal", "a.go")}, nil)
	out, err := execCerneStdin(t, stdin, "hook", "work-gate")
	if err != nil || out == "" {
		t.Fatalf("work-gate with an empty switch did not deny: err %v, output %q", err, out)
	}
}

// TestInstallAndGitHooksIgnoreSwitch (F-0360 crit. 5): install, commit-trailer
// and guard commit-msg behave as without CERNE_NO_AGENT_HOOKS.
func TestInstallAndGitHooksIgnoreSwitch(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	enableCommitTrailer(t, projDir)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	if err := app.BindSession(filepath.Dir(repoDir), "sess-a", "W-0001"); err != nil {
		t.Fatal(err)
	}
	putFeature(t, repoDir, "F-0001", domain.StateApproved)
	t.Setenv("CERNE_NO_AGENT_HOOKS", "1")

	if out, err := execCerne(t, "hook", "install", "--dir", projDir); err != nil {
		t.Fatalf("hook install: %v\noutput: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(projDir, ".claude", "hooks", "cerne-work-gate.sh")); err != nil {
		t.Fatalf("hook install did not write the scripts with the switch on: %v", err)
	}

	p := writeMsgFile(t, projDir, "feat: coisa\n")
	if out, err := execCerneStdin(t, "", "hook", "commit-trailer", p); err != nil {
		t.Fatalf("hook commit-trailer: %v\noutput: %s", err, out)
	}
	if got, _ := os.ReadFile(p); !strings.Contains(string(got), "Cerne: W-0001") {
		t.Fatalf("commit-trailer did not stamp with the switch on:\n%s", got)
	}

	p = writeMsgFile(t, projDir, "feat: coisa (F-0001)\n")
	if out, err := execCerneStdin(t, "", "guard", "commit-msg", p); err == nil {
		t.Fatalf("guard commit-msg let an approved feature pass with the switch on\noutput: %s", out)
	}
}
