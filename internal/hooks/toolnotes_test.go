package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type settingsHooks struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
}

func readSettingsHooks(t *testing.T, path string) (settingsHooks, []byte) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s settingsHooks
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	return s, b
}

// wired reports whether event carries an entry with matcher whose command
// contains cmd.
func (s settingsHooks) wired(event, matcher, cmd string) bool {
	for _, e := range s.Hooks[event] {
		for _, h := range e.Hooks {
			if e.Matcher == matcher && strings.Contains(h.Command, cmd) {
				return true
			}
		}
	}
	return false
}

// TestInstall_WiresToolNotesBefore (F-0457 criterion 8): Claude Code gets the
// tool-notes script under PreToolUse/Bash, idempotently, next to the work
// gate; the asset table lists it, so a project installed before it is told
// its hooks are stale (F-0405).
func TestInstall_WiresToolNotesBefore(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(dir, ".claude", "hooks", toolNotesScript))
	if err != nil {
		t.Fatalf("tool-notes script not installed: %v", err)
	}
	for _, want := range []string{"cerne hook tool-notes", "exit 0", "CERNE_NO_AGENT_HOOKS"} {
		if !strings.Contains(string(script), want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	settings := filepath.Join(dir, ".claude", "settings.json")
	s, b := readSettingsHooks(t, settings)
	if !s.wired("PreToolUse", "Bash", toolNotesScript) || !s.wired("PreToolUse", "Edit|Write", gateScript) {
		t.Fatalf("PreToolUse wiring:\n%s", b)
	}
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(settings); string(again) != string(b) {
		t.Fatalf("second install changed settings.json:\n%s", again)
	}
	if !slices.ContainsFunc(Assets(), func(a Asset) bool {
		return a.Script == toolNotesScript && a.Event == "PreToolUse" && a.Matcher == "Bash"
	}) {
		t.Fatal("Assets() does not list the tool-notes hook")
	}
	if err := os.Remove(filepath.Join(dir, ".claude", "hooks", toolNotesScript)); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(Stale(dir), toolNotesScript) {
		t.Fatalf("Stale = %v, want it to name %s", Stale(dir), toolNotesScript)
	}
}

// TestInstallCodex_WiresToolNotes (F-0457 criterion 8): Codex runs tool-notes
// before a Bash call, in its own format.
func TestInstallCodex_WiresToolNotes(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir, ProviderCodex); err != nil {
		t.Fatal(err)
	}
	s, b := readSettingsHooks(t, filepath.Join(dir, ".codex", "hooks.json"))
	if !s.wired("PreToolUse", "Bash", "cerne hook tool-notes --format codex") {
		t.Fatalf("codex PreToolUse:\n%s", b)
	}
}
