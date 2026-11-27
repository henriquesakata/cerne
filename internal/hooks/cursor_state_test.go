package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestCursorRuleState (F-0526 crit. 3): missing/ok/divergent for .cursor/rules/cerne.mdc.
func TestCursorRuleState(t *testing.T) { // F-0526 crit. 3
	proj := t.TempDir()

	if got := CursorRuleState(proj); got != FileMissing {
		t.Errorf("absent .cursor/rules/cerne.mdc = %q, want missing", got)
	}

	want := CursorRule()
	rulesDst := filepath.Join(proj, ".cursor", "rules", "cerne.mdc")
	if err := os.MkdirAll(filepath.Dir(rulesDst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rulesDst, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CursorRuleState(proj); got != FileOK {
		t.Errorf("canonical .cursor/rules/cerne.mdc = %q, want ok", got)
	}

	if err := os.WriteFile(rulesDst, []byte("divergent content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CursorRuleState(proj); got != FileDivergent {
		t.Errorf("divergent .cursor/rules/cerne.mdc = %q, want divergent", got)
	}

	// Unreadable file: treat as ok (fail-open, same as CopilotHooksState).
	if err := os.Chmod(rulesDst, 0); err != nil {
		t.Skip("chmod 0 not supported")
	}
	defer os.Chmod(rulesDst, 0o644)
	if got := CursorRuleState(proj); got != FileOK {
		t.Errorf("unreadable .cursor/rules/cerne.mdc = %q, want ok (fail-open)", got)
	}
}

// TestCursorHooksState (F-0526 crit. 3): missing/ok/divergent for .cursor/hooks.json.
func TestCursorHooksState(t *testing.T) { // F-0526 crit. 3
	proj := t.TempDir()
	hooksPath := filepath.Join(proj, ".cursor", "hooks.json")

	// Missing file → missing.
	if got := CursorHooksState(proj); got != FileMissing {
		t.Errorf("absent .cursor/hooks.json = %q, want missing", got)
	}

	writeHooks := func(sessionStart []map[string]any) {
		t.Helper()
		doc := map[string]any{"version": 1, "hooks": map[string]any{"sessionStart": sessionStart}}
		raw, _ := json.MarshalIndent(doc, "", "  ")
		if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(hooksPath, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Valid JSON with the managed sessionStart command → ok.
	managedCmd := failOpenCmd("cerne hook session-context --format cursor")
	writeHooks([]map[string]any{{"command": managedCmd}})
	if got := CursorHooksState(proj); got != FileOK {
		t.Errorf("hooks.json with managed sessionStart = %q, want ok", got)
	}

	// Extra entries alongside managed one → still ok.
	writeHooks([]map[string]any{
		{"command": "echo third-party"},
		{"command": managedCmd},
	})
	if got := CursorHooksState(proj); got != FileOK {
		t.Errorf("hooks.json with extra sessionStart entry = %q, want ok", got)
	}

	// Missing managed command → divergent.
	writeHooks([]map[string]any{{"command": "echo no-cerne"}})
	if got := CursorHooksState(proj); got != FileDivergent {
		t.Errorf("hooks.json missing managed sessionStart = %q, want divergent", got)
	}

	// No sessionStart key at all → divergent.
	raw, _ := json.Marshal(map[string]any{"version": 1, "hooks": map[string]any{}})
	if err := os.WriteFile(hooksPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CursorHooksState(proj); got != FileDivergent {
		t.Errorf("hooks.json with empty hooks = %q, want divergent", got)
	}

	// Invalid JSON → divergent.
	if err := os.WriteFile(hooksPath, []byte("{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CursorHooksState(proj); got != FileDivergent {
		t.Errorf("invalid JSON .cursor/hooks.json = %q, want divergent", got)
	}
}

// TestCursorSandboxState (F-0526 crit. 3): when no extra dirs are required, absent
// or valid JSON → ok; invalid JSON → divergent. When dirs are required, absent → missing.
func TestCursorSandboxState(t *testing.T) { // F-0526 crit. 3
	proj := t.TempDir()
	sandboxPath := filepath.Join(proj, ".cursor", "sandbox.json")

	// Without .cernerc there are no expected dirs.
	// Absent file → ok.
	if got := CursorSandboxState(proj); got != FileOK {
		t.Errorf("no .cernerc, absent sandbox.json = %q, want ok", got)
	}

	// Present valid JSON but no required dirs → ok.
	if err := os.MkdirAll(filepath.Dir(sandboxPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sandboxPath, []byte(`{"additionalReadwritePaths":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CursorSandboxState(proj); got != FileOK {
		t.Errorf("no .cernerc, valid sandbox.json = %q, want ok", got)
	}

	// Invalid JSON → divergent even when no dirs required.
	if err := os.WriteFile(sandboxPath, []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CursorSandboxState(proj); got != FileDivergent {
		t.Errorf("invalid JSON sandbox.json (no .cernerc) = %q, want divergent", got)
	}
}

// TestCursorMCPState (F-0526 crit. 3): missing/ok/divergent for .cursor/mcp.json.
func TestCursorMCPState(t *testing.T) { // F-0526 crit. 3
	proj := t.TempDir()
	mcpPath := filepath.Join(proj, ".cursor", "mcp.json")

	// Absent file → missing.
	if got := CursorMCPState(proj); got != FileMissing {
		t.Errorf("absent .cursor/mcp.json = %q, want missing", got)
	}

	writeMCP := func(servers map[string]any) {
		t.Helper()
		doc := map[string]any{"mcpServers": servers}
		raw, _ := json.MarshalIndent(doc, "", "  ")
		if err := os.MkdirAll(filepath.Dir(mcpPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mcpPath, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// File with canonical cerne server → ok.
	server := cursorMCPServer()
	writeMCP(map[string]any{"cerne": server})
	if got := CursorMCPState(proj); got != FileOK {
		t.Errorf("canonical .cursor/mcp.json = %q, want ok", got)
	}

	// Extra servers beside cerne → still ok.
	writeMCP(map[string]any{
		"cerne":  server,
		"other":  map[string]any{"command": "other"},
	})
	if got := CursorMCPState(proj); got != FileOK {
		t.Errorf("mcp.json with extra servers = %q, want ok", got)
	}

	// cerne key absent → missing.
	writeMCP(map[string]any{"other": map[string]any{"command": "x"}})
	if got := CursorMCPState(proj); got != FileMissing {
		t.Errorf(".cursor/mcp.json without cerne key = %q, want missing", got)
	}

	// cerne key with wrong content → divergent.
	writeMCP(map[string]any{"cerne": map[string]any{"command": "wrong"}})
	if got := CursorMCPState(proj); got != FileDivergent {
		t.Errorf("mcp.json with wrong cerne entry = %q, want divergent", got)
	}

	// Invalid JSON → divergent.
	if err := os.WriteFile(mcpPath, []byte("{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := CursorMCPState(proj); got != FileDivergent {
		t.Errorf("invalid JSON .cursor/mcp.json = %q, want divergent", got)
	}
}

// TestStaleIncludesCursorRuleDrift (F-0526 crit. 4): hooks.Stale reports
// .cursor/rules/cerne.mdc when it exists and diverges, not when absent.
func TestStaleIncludesCursorRuleDrift(t *testing.T) { // F-0526 crit. 4
	proj := t.TempDir()
	if _, err := Install(proj); err != nil {
		t.Fatal(err)
	}

	// No .cursor/rules/cerne.mdc — not nagged.
	for _, s := range Stale(proj) {
		if s == "cursor: .cursor/rules/cerne.mdc" {
			t.Error("Stale reports cursor rule when file is absent (no nagging)")
		}
	}

	// Divergent content → reported.
	rulesDst := filepath.Join(proj, ".cursor", "rules", "cerne.mdc")
	if err := os.MkdirAll(filepath.Dir(rulesDst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rulesDst, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range Stale(proj) {
		if s == "cursor: .cursor/rules/cerne.mdc" {
			found = true
		}
	}
	if !found {
		t.Error("Stale does not report divergent .cursor/rules/cerne.mdc")
	}

	// Canonical content → not reported.
	if err := os.WriteFile(rulesDst, []byte(CursorRule()), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, s := range Stale(proj) {
		if s == "cursor: .cursor/rules/cerne.mdc" {
			t.Error("Stale reports cursor rule when it is canonical")
		}
	}
}
