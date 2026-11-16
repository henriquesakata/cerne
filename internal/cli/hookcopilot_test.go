package cli

import (
	"encoding/json"
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
