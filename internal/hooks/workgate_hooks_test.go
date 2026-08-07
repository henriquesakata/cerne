package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstall_WiresWorkBindAndGate (F-0151): install lays down both plumbing
// scripts and wires them — bind under PostToolUse/Bash, gate under
// PreToolUse/Edit|Write — idempotently.
func TestInstall_WiresWorkBindAndGate(t *testing.T) {
	projDir := t.TempDir()
	if _, err := Install(projDir); err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, script := range []string{bindScript, gateScript} {
		data, err := os.ReadFile(filepath.Join(projDir, ".claude", "hooks", script))
		if err != nil {
			t.Fatalf("%s missing: %v", script, err)
		}
		content := string(data)
		if !strings.Contains(content, "exit 0") || !strings.Contains(content, "cerne hook") {
			t.Errorf("%s must delegate to a cerne subcommand and fail open:\n%s", script, content)
		}
	}

	raw, err := os.ReadFile(filepath.Join(projDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json missing: %v", err)
	}
	var root struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("settings.json invalid: %v\n%s", err, raw)
	}
	assertWired := func(event, matcher, script string) {
		t.Helper()
		for _, e := range root.Hooks[event] {
			for _, h := range e.Hooks {
				if strings.Contains(h.Command, script) {
					if e.Matcher != matcher {
						t.Errorf("%s/%s wired with matcher %q, want %q", event, script, e.Matcher, matcher)
					}
					return
				}
			}
		}
		t.Errorf("%s does not wire %s:\n%s", event, script, raw)
	}
	assertWired("PostToolUse", "Bash", bindScript)
	assertWired("PreToolUse", "Edit|Write", gateScript)

	results, err := Install(projDir)
	if err != nil {
		t.Fatalf("Install (2nd): %v", err)
	}
	for _, r := range results {
		if r.Asset == trailerHookName || r.Asset == guardHookName {
			continue // skipped outside a real git repo (F-0173) — has its own test
		}
		if r.Status != StatusUnchanged {
			t.Errorf("second install: %s = %s, want unchanged", r.Asset, r.Status)
		}
	}
}
