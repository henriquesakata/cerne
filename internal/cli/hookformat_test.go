package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestHookWorkGateFormatCursor (F-0174 crit. 5): --format cursor emits the
// Cursor native deny envelope for an unbound session.
func TestHookWorkGateFormatCursor(t *testing.T) {
	projDir, _, _ := setupProject(t)
	enableWorkGate(t, projDir)
	stdin := hookJSON(t, "sess-cur", projDir, "Edit",
		map[string]any{"file_path": filepath.Join(projDir, "internal", "a.go")}, nil)
	out, err := execCerneStdin(t, stdin, "hook", "work-gate", "--format", "cursor")
	if err != nil {
		t.Fatalf("work-gate cursor: %v\noutput: %s", err, out)
	}
	for _, want := range []string{`"permission":"deny"`, "agent_message", "cerne work start"} {
		if !strings.Contains(out, want) {
			t.Fatalf("cursor deny missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "hookSpecificOutput") {
		t.Fatalf("cursor deny must not carry the claude envelope:\n%s", out)
	}
}

// TestHookWorkGateFormatGemini (F-0174 crit. 5): --format gemini emits the
// Gemini BeforeTool deny envelope; session and cwd fall back to the
// GEMINI_SESSION_ID / GEMINI_CWD env the hook runtime provides.
func TestHookWorkGateFormatGemini(t *testing.T) {
	projDir, _, _ := setupProject(t)
	enableWorkGate(t, projDir)
	t.Setenv("GEMINI_SESSION_ID", "sess-gem")
	t.Setenv("GEMINI_CWD", projDir)
	stdin := hookJSON(t, "", "", "write_file",
		map[string]any{"file_path": filepath.Join(projDir, "internal", "a.go")}, nil)
	out, err := execCerneStdin(t, stdin, "hook", "work-gate", "--format", "gemini")
	if err != nil {
		t.Fatalf("work-gate gemini: %v\noutput: %s", err, out)
	}
	for _, want := range []string{`"decision":"deny"`, `"reason"`, "cerne work start"} {
		if !strings.Contains(out, want) {
			t.Fatalf("gemini deny missing %q:\n%s", want, out)
		}
	}
}

// TestHookWorkGateFormatDefaultUnchanged: no flag keeps the claude envelope.
func TestHookWorkGateFormatDefaultUnchanged(t *testing.T) {
	projDir, _, _ := setupProject(t)
	enableWorkGate(t, projDir)
	stdin := hookJSON(t, "sess-x", projDir, "Edit",
		map[string]any{"file_path": filepath.Join(projDir, "internal", "a.go")}, nil)
	out, err := execCerneStdin(t, stdin, "hook", "work-gate")
	if err != nil {
		t.Fatalf("work-gate: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "hookSpecificOutput") {
		t.Fatalf("default format changed:\n%s", out)
	}
}

// TestHookSessionContext (F-0174 crit. 4): emits the registration rules plus
// the live work list, in the requested provider's envelope.
func TestHookSessionContext(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "work", "start", "Investigar erro X", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\noutput: %s", err, out)
	}
	payload := hookJSON(t, "s", projDir, "", nil, nil)

	for format, wants := range map[string][]string{
		"claude": {"hookSpecificOutput", `"SessionStart"`, "cerne work start", "W-0001"},
		"codex":  {"hookSpecificOutput", `"SessionStart"`, "W-0001"},
		"cursor": {`"additional_context"`, "W-0001"},
		"gemini": {"hookSpecificOutput", "W-0001"},
	} {
		out, err := execCerneStdin(t, payload, "hook", "session-context", "--format", format)
		if err != nil {
			t.Fatalf("%s: %v\noutput: %s", format, err, out)
		}
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Fatalf("%s missing %q:\n%s", format, want, out)
			}
		}
	}

	// Outside a cerne project: silence.
	out, err := execCerneStdin(t, hookJSON(t, "s", t.TempDir(), "", nil, nil), "hook", "session-context")
	if err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("non-cerne project: err=%v out=%s", err, out)
	}
}
