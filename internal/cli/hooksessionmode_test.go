package cli

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
)

// TestHooksRecordPermissionMode (F-0491 crit. 2): every Claude Code contact
// point that carries permission_mode (R-0034: UserPromptSubmit, PreToolUse,
// Stop — not SessionStart) records the session's last mode with its harness;
// a subagent's hook records nothing.
func TestHooksRecordPermissionMode(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	cacheDir := filepath.Dir(repoDir)
	t.Setenv("CLAUDECODE", "1")
	payload := func(extra map[string]any) string {
		p := map[string]any{"session_id": "sess-m", "cwd": projDir}
		for k, v := range extra {
			p[k] = v
		}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	for _, tc := range []struct {
		name  string
		args  []string
		extra map[string]any
	}{
		{"prompt-notes", []string{"hook", "prompt-notes"}, map[string]any{"hook_event_name": "UserPromptSubmit", "prompt": "hi", "permission_mode": "auto"}},
		{"tool-notes", []string{"hook", "tool-notes"}, map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}, "permission_mode": "acceptEdits"}},
		{"work-gate", []string{"hook", "work-gate"}, map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Edit", "tool_input": map[string]any{"file_path": filepath.Join(projDir, "x.go")}, "permission_mode": "default"}},
		{"stop-check", []string{"hook", "stop-check"}, map[string]any{"hook_event_name": "Stop", "permission_mode": "bypassPermissions"}},
	} {
		if _, err := execCerneStdin(t, payload(tc.extra), tc.args...); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got, ok := app.SessionModeOf(cacheDir, "sess-m")
		if want := (app.SessionMode{Harness: "claude-code", Mode: tc.extra["permission_mode"].(string)}); !ok || got != want {
			t.Fatalf("%s: mode %+v (%v), want %+v", tc.name, got, ok, want)
		}
	}
	// A subagent's hook shares the session id but not necessarily its mode.
	if _, err := execCerneStdin(t, payload(map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}, "agent_id": "a1", "permission_mode": "auto"}), "hook", "tool-notes"); err != nil {
		t.Fatal(err)
	}
	if got, _ := app.SessionModeOf(cacheDir, "sess-m"); got.Mode != "bypassPermissions" {
		t.Fatalf("a subagent's hook overwrote the main thread's mode: %+v", got)
	}
}
