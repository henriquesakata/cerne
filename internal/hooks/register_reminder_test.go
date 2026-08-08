package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstall_WritesRegisterReminder (F-0139 crit. 1): install lays down the
// SessionStart reminder script, executable content carrying the three rules —
// feature registration, work registration/listing, human gates.
func TestInstall_WritesRegisterReminder(t *testing.T) {
	projDir := t.TempDir()
	if _, err := Install(projDir); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(projDir, ".claude", "hooks", registerScript))
	if err != nil {
		t.Fatalf("reminder script missing: %v", err)
	}
	content := string(data)
	for _, rule := range []string{"cerne feature new", "cerne work start", "cerne work list", "cerne feature approve", "accept"} {
		if !strings.Contains(content, rule) {
			t.Errorf("reminder script missing rule fragment %q", rule)
		}
	}
}

// TestInstall_WiresSessionStart (crit. 2): settings.json gains a SessionStart
// entry running the reminder, and a re-run is idempotent.
func TestInstall_WiresSessionStart(t *testing.T) {
	projDir := t.TempDir()
	if _, err := Install(projDir); err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(projDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json missing: %v", err)
	}
	if !strings.Contains(string(raw), registerScript) {
		t.Fatalf("settings.json does not wire %s:\n%s", registerScript, raw)
	}

	// Idempotent: a second run changes nothing.
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

// TestInstall_PreservesExistingSessionStart (crit. 2): an existing SessionStart
// hook survives the merge alongside the new reminder entry.
func TestInstall_PreservesExistingSessionStart(t *testing.T) {
	projDir := t.TempDir()
	settings := filepath.Join(projDir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	existing := `{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/custom.sh\""
          }
        ]
      }
    ]
  }
}
`
	if err := os.WriteFile(settings, []byte(existing), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Install(projDir); err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, err := os.ReadFile(settings)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var root struct {
		Hooks struct {
			SessionStart []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"SessionStart"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("settings.json invalid after merge: %v\n%s", err, raw)
	}
	var haveCustom, haveReminder bool
	for _, e := range root.Hooks.SessionStart {
		for _, h := range e.Hooks {
			if strings.Contains(h.Command, "custom.sh") {
				haveCustom = true
			}
			if strings.Contains(h.Command, registerScript) {
				haveReminder = true
			}
		}
	}
	if !haveCustom || !haveReminder {
		t.Fatalf("custom=%v reminder=%v, want both:\n%s", haveCustom, haveReminder, raw)
	}
}
