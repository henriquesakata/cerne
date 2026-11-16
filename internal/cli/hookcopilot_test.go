package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
)

// decodeTop parses a hook answer and fails when it is not one JSON object.
func decodeTop(t *testing.T, out string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, out)
	}
	return m
}

// TestHookWorkGateFormatCopilot (F-0444 crit. 1): the deny rides the top-level
// permissionDecision of Copilot's PreToolUse; the path may arrive as `path`.
func TestHookWorkGateFormatCopilot(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	enableWorkGate(t, projDir)
	file := filepath.Join(projDir, "internal", "a.go")

	for name, input := range map[string]map[string]any{
		"file_path": {"file_path": file},
		"path":      {"path": file},
	} {
		out, err := execCerneStdin(t, hookJSON(t, "sess-cp", projDir, "Edit", input, nil), "hook", "work-gate", "--format", "copilot")
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, out)
		}
		m := decodeTop(t, out)
		if m["permissionDecision"] != "deny" || !strings.Contains(m["permissionDecisionReason"].(string), "cerne work start") {
			t.Fatalf("%s: deny envelope wrong:\n%s", name, out)
		}
		if _, nested := m["hookSpecificOutput"]; nested {
			t.Fatalf("%s: copilot deny must be top-level:\n%s", name, out)
		}
	}

	// A bound session passes silently.
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\n%s", err, out)
	}
	if err := app.BindSession(filepath.Dir(repoDir), "sess-cp", "W-0001"); err != nil {
		t.Fatal(err)
	}
	out, err := execCerneStdin(t, hookJSON(t, "sess-cp", projDir, "Edit", map[string]any{"path": file}, nil), "hook", "work-gate", "--format", "copilot")
	if err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("bound session must be silent: %v\n%s", err, out)
	}
}

// TestHookSessionContextFormatCopilot: the board is top-level additionalContext.
func TestHookSessionContextFormatCopilot(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := execCerneStdin(t, sessionPayload("sess-cp", projDir, ""), "hook", "session-context", "--format", "copilot")
	if err != nil {
		t.Fatal(err)
	}
	m := decodeTop(t, out)
	text, _ := m["additionalContext"].(string)
	if !strings.Contains(text, "ACTIVE WORK RIGHT NOW") {
		t.Fatalf("board missing from additionalContext:\n%s", out)
	}
	if _, nested := m["hookSpecificOutput"]; nested {
		t.Fatalf("copilot context must be top-level:\n%s", out)
	}
}

// TestHookProvenanceFormatCopilot: the PostToolUse reminder is top-level too.
func TestHookProvenanceFormatCopilot(t *testing.T) {
	projDir, _, _ := setupProject(t)
	stdin := hookJSON(t, "s", projDir, "Bash",
		map[string]any{"command": "cerne feature complete F-0001"}, map[string]any{"stdout": "ok"})
	out, err := execCerneStdin(t, stdin, "hook", "provenance", "--format", "copilot")
	if err != nil {
		t.Fatal(err)
	}
	m := decodeTop(t, out)
	if text, _ := m["additionalContext"].(string); !strings.Contains(text, "cerne provenance") {
		t.Fatalf("reminder missing:\n%s", out)
	}
}

// Payloads below are what Copilot CLI 1.0.112 sent in the F-0444 probe: file
// creation is tool "Edit" whose tool_input is the apply_patch text itself, and
// PostToolUse carries tool_result, not tool_response.
func copilotPayload(sess, cwd, event, tool, toolInput, extra string) string {
	return `{"hook_event_name":"` + event + `","session_id":"` + sess + `","timestamp":"2026-11-16T17:48:42.443Z","cwd":` + jsonString(cwd) +
		`,"tool_use_id":"call_x","tool_name":"` + tool + `","tool_input":` + toolInput + extra + `}`
}

// TestHookWorkGateCopilotPatchPayload: a patch-text tool_input names its files
// in "*** Add/Update File:" lines; the gate denies an unbound session for them.
func TestHookWorkGateCopilotPatchPayload(t *testing.T) {
	projDir, _, _ := setupProject(t)
	enableWorkGate(t, projDir)
	patch := jsonString("*** Begin Patch\n*** Add File: internal/a.go\n+hi\n*** End Patch\n")
	out, err := execCerneStdin(t, copilotPayload("sess-p", projDir, "PreToolUse", "Edit", patch, ""), "hook", "work-gate", "--format", "copilot")
	if err != nil {
		t.Fatal(err)
	}
	if m := decodeTop(t, out); m["permissionDecision"] != "deny" {
		t.Fatalf("patch edit of a project file not denied:\n%s", out)
	}

	// A patch touching only an allowed path passes.
	docs := jsonString("*** Begin Patch\n*** Update File: .docs/n.md\n@@\n-a\n+b\n*** End Patch\n")
	out, err = execCerneStdin(t, copilotPayload("sess-p", projDir, "PreToolUse", "Edit", docs, ""), "hook", "work-gate", "--format", "copilot")
	if err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf(".docs patch must pass silently: %v\n%s", err, out)
	}
}

// TestHookBindWorkCopilotToolResult: the minted W- id is read from
// tool_result.text_result_for_llm.
func TestHookBindWorkCopilotToolResult(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\n%s", err, out)
	}
	cmdJSON := `{"command":"cerne work start \"Investigar erro X\"","description":"x"}`
	res := `,"tool_result":{"result_type":"success","text_result_for_llm":"W-0001 created (active) — Investigar erro X (actor: agent)\n<shellId: 0 completed with exit code 0>"}`
	if out, err := execCerneStdin(t, copilotPayload("sess-cp-bind", projDir, "PostToolUse", "Bash", cmdJSON, res), "hook", "bind-work", "--format", "copilot"); err != nil {
		t.Fatalf("bind-work: %v\n%s", err, out)
	}
	if id, ok, err := app.CurrentBinding(filepath.Dir(repoDir), repoDir, "sess-cp-bind", ""); err != nil || !ok || id != "W-0001" {
		t.Fatalf("session not bound: (%q, %v, %v)", id, ok, err)
	}
}

// TestHookSessionContextCopilotStale (F-0444 crit. 5): a Copilot session start
// runs the F-0405 check over the Copilot files, so a divergent hooks file is
// flagged in the context the agent receives.
func TestHookSessionContextCopilotStale(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if err := os.MkdirAll(filepath.Join(projDir, ".github", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, ".github", "hooks", "cerne.json"), []byte(`{"version":1,"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execCerneStdin(t, sessionPayload("sess-st", projDir, ""), "hook", "session-context", "--format", "copilot")
	if err != nil {
		t.Fatal(err)
	}
	text, _ := decodeTop(t, out)["additionalContext"].(string)
	if !strings.Contains(text, "out of date with this cerne build") {
		t.Fatalf("stale copilot file not flagged:\n%s", text)
	}
}

func gateCopilot(t *testing.T, projDir, cwd, toolInput string) string {
	t.Helper()
	out, err := execCerneStdin(t, copilotPayload("sess-g", cwd, "PreToolUse", "Edit", toolInput, ""), "hook", "work-gate", "--format", "copilot")
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func denied(t *testing.T, out string) bool {
	t.Helper()
	if strings.TrimSpace(out) == "" {
		return false
	}
	return decodeTop(t, out)["permissionDecision"] == "deny"
}

// TestHookWorkGateCopilotPatchShapes (F-0444 critic items 4/5): every file a
// patch names is gated — Move to, Delete, any one of several — paths resolve
// against the project, never stay relative, and a patch we cannot parse fails
// open.
func TestHookWorkGateCopilotPatchShapes(t *testing.T) {
	projDir, _, _ := setupProject(t)
	enableWorkGate(t, projDir, "docs/**")
	patch := func(lines ...string) string {
		return jsonString("*** Begin Patch\n" + strings.Join(lines, "\n") + "\n*** End Patch\n")
	}
	outside := filepath.Join(t.TempDir(), "x.go")
	for name, tc := range map[string]struct {
		input string
		deny  bool
	}{
		"move into a gated path":       {patch("*** Update File: docs/a.md", "*** Move to: internal/a.go", "@@", "-a", "+b"), true},
		"move out of gated, to docs":   {patch("*** Update File: internal/a.go", "*** Move to: docs/a.md", "@@", "-a", "+b"), true},
		"delete":                       {patch("*** Delete File: internal/a.go"), true},
		"multi-file, one denied":       {patch("*** Update File: docs/a.md", "@@", "-a", "+b", "*** Add File: internal/b.go", "+x"), true},
		"multi-file, all allowed":      {patch("*** Update File: docs/a.md", "@@", "-a", "+b", "*** Add File: .docs/n.md", "+x"), false},
		"dot-dot traversal inside":     {patch("*** Add File: docs/../internal/a.go", "+x"), true},
		"dot-dot escaping the project": {patch("*** Add File: ../outside.go", "+x"), false},
		"absolute path outside":        {patch("*** Add File: " + outside, "+x"), false},
		"unparseable patch":            {jsonString("this is not a patch"), false},
		"object path, gated":           {`{"path":"internal/a.go"}`, true},
	} {
		if got := denied(t, gateCopilot(t, projDir, projDir, tc.input)); got != tc.deny {
			t.Errorf("%s: deny=%v, want %v", name, got, tc.deny)
		}
	}

	// A relative path with no cwd in the payload resolves against
	// COPILOT_PROJECT_DIR, then the process cwd — never stays relative.
	rel := patch("*** Add File: internal/a.go", "+x")
	t.Setenv("COPILOT_PROJECT_DIR", projDir)
	if !denied(t, gateCopilot(t, projDir, "", rel)) {
		t.Error("empty cwd + COPILOT_PROJECT_DIR: relative path not gated")
	}
	t.Setenv("COPILOT_PROJECT_DIR", "")
	t.Chdir(projDir)
	if !denied(t, gateCopilot(t, projDir, "", rel)) {
		t.Error("empty cwd + process cwd: relative path not gated")
	}
}
