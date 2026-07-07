package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/project"
)

// execCerneStdin runs the CLI with args feeding stdin — the hook plumbing
// subcommands read their payload there.
func execCerneStdin(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	root := Root()
	out := &bytes.Buffer{}
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func hookJSON(t *testing.T, sessionID, cwd, toolName string, toolInput, toolResponse any) string {
	t.Helper()
	payload := map[string]any{
		"session_id": sessionID,
		"cwd":        cwd,
		"tool_name":  toolName,
		"tool_input": toolInput,
	}
	if toolResponse != nil {
		payload["tool_response"] = toolResponse
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal hook payload: %v", err)
	}
	return string(raw)
}

// enableWorkGate rewrites the project's .cernerc with [gates] work_gate on.
func enableWorkGate(t *testing.T, projDir string, extraAllow ...string) {
	t.Helper()
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Gates.WorkGate = "on"
	cfg.Gates.WorkGateAllow = extraAllow
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

// TestHookBindWorkBindsFromWorkStart (F-0151 crit. 1): a successful
// `cerne work start` seen by the PostToolUse hook binds the session to the
// minted W- id.
func TestHookBindWorkBindsFromWorkStart(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}

	stdin := hookJSON(t, "sess-hook", projDir, "Bash",
		map[string]any{"command": `cerne work start "Investigar erro X"`},
		map[string]any{"stdout": "W-0001 criada (active) — Investigar erro X (ator: agent)"})
	if out, err := execCerneStdin(t, stdin, "hook", "bind-work"); err != nil {
		t.Fatalf("hook bind-work: %v\noutput: %s", err, out)
	}

	cacheDir := filepath.Dir(repoDir)
	id, ok, err := app.CurrentBinding(cacheDir, repoDir, "sess-hook")
	if err != nil {
		t.Fatalf("CurrentBinding: %v", err)
	}
	if !ok || id != "W-0001" {
		t.Fatalf("binding = (%q, %v), want (W-0001, true)", id, ok)
	}
}

// TestHookBindWorkIgnoresOtherCommands: unrelated Bash calls, and hook input
// that is not even JSON, bind nothing and never error (fail-open plumbing).
func TestHookBindWorkIgnoresOtherCommands(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	for name, stdin := range map[string]string{
		"other command": hookJSON(t, "sess-a", projDir, "Bash",
			map[string]any{"command": "go test ./..."}, map[string]any{"stdout": "ok W-0001"}),
		"failed work start (no id in response)": hookJSON(t, "sess-a", projDir, "Bash",
			map[string]any{"command": "cerne work start x"}, map[string]any{"stdout": "Error: similar exists"}),
		"not json": "garbage{",
	} {
		if out, err := execCerneStdin(t, stdin, "hook", "bind-work"); err != nil {
			t.Fatalf("%s: hook bind-work errored: %v\noutput: %s", name, err, out)
		}
	}
	if _, ok, _ := app.CurrentBinding(filepath.Dir(repoDir), repoDir, "sess-a"); ok {
		t.Fatal("nothing should have been bound")
	}
}

// TestHookWorkGateDeniesUnbound (F-0151 crit. 2): gate on + project file +
// unbound session -> deny JSON with an actionable reason.
func TestHookWorkGateDeniesUnbound(t *testing.T) {
	projDir, _, _ := setupProject(t)
	enableWorkGate(t, projDir)

	stdin := hookJSON(t, "sess-unbound", projDir, "Edit",
		map[string]any{"file_path": filepath.Join(projDir, "internal", "a.go")}, nil)
	out, err := execCerneStdin(t, stdin, "hook", "work-gate")
	if err != nil {
		t.Fatalf("hook work-gate: %v\noutput: %s", err, out)
	}
	var d gateDeny
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("deny output is not JSON: %v\noutput: %s", err, out)
	}
	if d.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("decision = %q, want deny:\n%s", d.HookSpecificOutput.PermissionDecision, out)
	}
	if !strings.Contains(d.HookSpecificOutput.PermissionDecisionReason, "cerne work start") {
		t.Fatalf("reason not actionable:\n%s", out)
	}
}

// TestHookWorkGateAllows (crit. 3/4): bound session, allowlisted paths, and a
// disabled gate all pass silently (no output at all).
func TestHookWorkGateAllows(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	enableWorkGate(t, projDir, "docs/**")
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	cacheDir := filepath.Dir(repoDir)
	if err := app.BindSession(cacheDir, "sess-bound", "W-0001"); err != nil {
		t.Fatalf("BindSession: %v", err)
	}

	codePath := filepath.Join(projDir, "internal", "a.go")
	cases := map[string]string{
		"bound session":   hookJSON(t, "sess-bound", projDir, "Edit", map[string]any{"file_path": codePath}, nil),
		".docs path":      hookJSON(t, "sess-x", projDir, "Write", map[string]any{"file_path": filepath.Join(projDir, ".docs", "n.md")}, nil),
		"extra allowlist": hookJSON(t, "sess-x", projDir, "Write", map[string]any{"file_path": filepath.Join(projDir, "docs", "n.md")}, nil),
		"outside project": hookJSON(t, "sess-x", projDir, "Edit", map[string]any{"file_path": filepath.Join(t.TempDir(), "s.md")}, nil),
	}
	for name, stdin := range cases {
		out, err := execCerneStdin(t, stdin, "hook", "work-gate")
		if err != nil {
			t.Fatalf("%s: %v\noutput: %s", name, err, out)
		}
		if strings.TrimSpace(out) != "" {
			t.Fatalf("%s: allow must be silent, got:\n%s", name, out)
		}
	}

	// Gate off (default config): even the unbound project file passes.
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Gates.WorkGate = ""
	cfg.Gates.WorkGateAllow = nil
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	stdin := hookJSON(t, "sess-x", projDir, "Edit", map[string]any{"file_path": codePath}, nil)
	out, err := execCerneStdin(t, stdin, "hook", "work-gate")
	if err != nil {
		t.Fatalf("gate off: %v\noutput: %s", err, out)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("gate off must be silent, got:\n%s", out)
	}
}

// TestHookBindWorkBindsFromFeatureStart (crit. 1): a confirmed
// `cerne feature start F-…` binds the F- id taken from the command.
func TestHookBindWorkBindsFromFeatureStart(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	stdin := hookJSON(t, "sess-f", projDir, "Bash",
		map[string]any{"command": "cerne feature start F-0002"},
		map[string]any{"stdout": "F-0002: approved → in_development"})
	if out, err := execCerneStdin(t, stdin, "hook", "bind-work"); err != nil {
		t.Fatalf("hook bind-work: %v\noutput: %s", err, out)
	}
	// The binding file exists even though F-0002 does not (CurrentBinding
	// would then read it as stale) — assert the raw bind happened.
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(repoDir), "sessions", "sess-f"))
	if err != nil {
		t.Fatalf("binding file: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != "F-0002" {
		t.Fatalf("bound id = %q, want F-0002", got)
	}
}
