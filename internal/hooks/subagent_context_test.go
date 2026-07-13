package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstall_WritesSubagentContext (F-0150 crit. 2/3): install lays down the
// SubagentStart context script — it must carry the condensed cerne rules, run
// `cerne work list` for the live board, emit the documented JSON envelope, and
// degrade silently (exit 0, no output) when cerne or the project is absent.
func TestInstall_WritesSubagentContext(t *testing.T) {
	projDir := t.TempDir()
	if _, err := Install(projDir); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(projDir, ".claude", "hooks", subagentScript))
	if err != nil {
		t.Fatalf("subagent context script missing: %v", err)
	}
	content := string(data)
	for _, frag := range []string{
		"cerne work list",  // live board injected
		"cerne work start", // registration rule
		"hookSpecificOutput",
		"additionalContext",
		"SubagentStart",
		"exit 0", // silent degradation path
	} {
		if !strings.Contains(content, frag) {
			t.Errorf("subagent context script missing fragment %q", frag)
		}
	}
}

// TestInstall_WiresSubagentStart (F-0150 crit. 1): settings.json gains a
// SubagentStart entry running the context script, and a re-run is idempotent.
func TestInstall_WiresSubagentStart(t *testing.T) {
	projDir := t.TempDir()
	if _, err := Install(projDir); err != nil {
		t.Fatalf("Install: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(projDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json missing: %v", err)
	}
	var root struct {
		Hooks struct {
			SubagentStart []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"SubagentStart"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("settings.json invalid: %v\n%s", err, raw)
	}
	wired := false
	for _, e := range root.Hooks.SubagentStart {
		for _, h := range e.Hooks {
			if strings.Contains(h.Command, subagentScript) {
				wired = true
			}
		}
	}
	if !wired {
		t.Fatalf("settings.json does not wire %s under SubagentStart:\n%s", subagentScript, raw)
	}

	results, err := Install(projDir)
	if err != nil {
		t.Fatalf("Install (2nd): %v", err)
	}
	for _, r := range results {
		if r.Asset == trailerHookName {
			continue // skipped outside a real git repo (F-0173) — has its own test
		}
		if r.Status != StatusUnchanged {
			t.Errorf("second install: %s = %s, want unchanged", r.Asset, r.Status)
		}
	}
}

// TestInstall_PreservesExistingSubagentStart (F-0150 crit. 4): an existing
// SubagentStart hook from another tool survives the merge alongside ours.
func TestInstall_PreservesExistingSubagentStart(t *testing.T) {
	projDir := t.TempDir()
	settings := filepath.Join(projDir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	existing := `{
  "hooks": {
    "SubagentStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/other-tool.sh\""
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
			SubagentStart []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"SubagentStart"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("settings.json invalid after merge: %v\n%s", err, raw)
	}
	var haveOther, haveOurs bool
	for _, e := range root.Hooks.SubagentStart {
		for _, h := range e.Hooks {
			if strings.Contains(h.Command, "other-tool.sh") {
				haveOther = true
			}
			if strings.Contains(h.Command, subagentScript) {
				haveOurs = true
			}
		}
	}
	if !haveOther || !haveOurs {
		t.Fatalf("other=%v ours=%v, want both:\n%s", haveOther, haveOurs, raw)
	}
}
