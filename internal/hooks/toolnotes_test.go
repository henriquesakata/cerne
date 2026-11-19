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

// TestInstall_WiresToolNotesAfter (F-0457 criterion 8): the .claude settings
// carry a PostToolUse/Bash|Edit|Write entry for Cursor's third-party import;
// its script leaves at once outside Cursor (no CURSOR_VERSION), so Claude
// Code pays no process for it.
func TestInstall_WiresToolNotesAfter(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(dir, ".claude", "hooks", afterNotesScript))
	if err != nil {
		t.Fatalf("after-notes script not installed: %v", err)
	}
	for _, want := range []string{"cerne hook tool-notes --after", "CURSOR_VERSION", "CERNE_NO_AGENT_HOOKS", "exit 0"} {
		if !strings.Contains(string(script), want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	s, b := readSettingsHooks(t, filepath.Join(dir, ".claude", "settings.json"))
	if !s.wired("PostToolUse", "Bash|Edit|Write", afterNotesScript) {
		t.Fatalf("PostToolUse wiring:\n%s", b)
	}
	if !slices.ContainsFunc(Assets(), func(a Asset) bool {
		return a.Script == afterNotesScript && a.Event == "PostToolUse" && a.Matcher == "Bash|Edit|Write"
	}) {
		t.Fatal("Assets() does not list the after-notes hook")
	}
	stdout, _, called := runAsset(t, dir, afterNotesScript, "CURSOR_VERSION=")
	if called || stdout != "" {
		t.Fatalf("outside Cursor the script must not run cerne (called=%v, out %q)", called, stdout)
	}
	if _, _, called := runAsset(t, dir, afterNotesScript, "CURSOR_VERSION=3.25.6"); !called {
		t.Fatal("inside Cursor the script must run cerne")
	}
}

// TestInstallGeminiCopilot_WiresToolNotes (F-0457 criterion 8): Gemini's
// AfterTool and Copilot's PostToolUse run tool-notes --after in their format.
func TestInstallGeminiCopilot_WiresToolNotes(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir, ProviderGemini); err != nil {
		t.Fatal(err)
	}
	s, b := readSettingsHooks(t, filepath.Join(dir, ".gemini", "settings.json"))
	if !s.wired("AfterTool", "run_shell_command|write_file|replace", "cerne hook tool-notes --after --format gemini") {
		t.Fatalf("gemini AfterTool:\n%s", b)
	}
	if _, err := Install(dir, ProviderCopilot); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".github", "hooks", "cerne.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"Bash", "Edit|Write"} {
		if !copilotWired(t, raw, "PostToolUse", m, "cerne hook tool-notes --after --format copilot") {
			t.Fatalf("copilot PostToolUse %s:\n%s", m, raw)
		}
	}
}

// copilotWired reports whether Copilot's hook file runs invocation under
// event with matcher.
func copilotWired(t *testing.T, raw []byte, event, matcher, invocation string) bool {
	t.Helper()
	var doc struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Bash    string `json:"bash"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, e := range doc.Hooks[event] {
		if e.Matcher == matcher && strings.Contains(e.Bash, invocation) {
			return true
		}
	}
	return false
}

// TestInstallUpgradesProviderFiles (critic finding 14): a .codex/hooks.json or
// .gemini/settings.json written by a release before F-0457 gains the
// tool-notes entry once, keeps a team's own entry, and a reinstall changes
// nothing.
func TestInstallUpgradesProviderFiles(t *testing.T) {
	for _, tc := range []struct {
		p           Provider
		file, event string
		matcher     string
		cmd         string
		old         string
	}{
		{ProviderCodex, ".codex/hooks.json", "PreToolUse", "Bash", "cerne hook tool-notes --format codex",
			`{"hooks":{"PreToolUse":[{"matcher":"Edit|Write","hooks":[{"type":"command","command":"cerne hook work-gate"}]},{"matcher":"Bash","hooks":[{"type":"command","command":"echo team"}]}]}}`},
		{ProviderGemini, ".gemini/settings.json", "AfterTool", "run_shell_command|write_file|replace", "cerne hook tool-notes --after --format gemini",
			`{"hooks":{"AfterTool":[{"matcher":"run_shell_command","hooks":[{"type":"command","command":"cerne hook bind-work --format gemini"}]},{"matcher":"read_file","hooks":[{"type":"command","command":"echo team"}]}]}}`},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, filepath.FromSlash(tc.file))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(tc.old), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Install(dir, tc.p); err != nil {
			t.Fatal(err)
		}
		s, b := readSettingsHooks(t, path)
		if !s.wired(tc.event, tc.matcher, tc.cmd) || !strings.Contains(string(b), "echo team") {
			t.Fatalf("%s upgrade:\n%s", tc.p, b)
		}
		if n := strings.Count(string(b), tc.cmd); n != 1 {
			t.Fatalf("%s: tool-notes wired %d times:\n%s", tc.p, n, b)
		}
		if _, err := Install(dir, tc.p); err != nil {
			t.Fatal(err)
		}
		if again, _ := os.ReadFile(path); string(again) != string(b) {
			t.Fatalf("%s: reinstall changed the file:\n%s", tc.p, again)
		}
	}
}
