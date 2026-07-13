package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func statuses(rs []Result) map[string]Status {
	m := map[string]Status{}
	for _, r := range rs {
		m[r.Asset] = r.Status
	}
	return m
}

// TestInstallCodex (F-0174 crit. 2/4/5): full wiring — hooks.json with the
// five events, config.toml with CERNE_ACTOR — idempotent, degradations noted.
func TestInstallCodex(t *testing.T) {
	dir := t.TempDir()
	results, err := Install(dir, ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".codex", "hooks.json"))
	if err != nil {
		t.Fatalf("hooks.json not written: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("hooks.json invalid:\n%s", raw)
	}
	for _, want := range []string{"SessionStart", "PreToolUse", `Edit|Write`, "cerne hook work-gate", "cerne hook bind-work", "cerne hook lifecycle-reminder", "cerne hook provenance", "cerne hook stop-check", "session-context --format codex"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("hooks.json missing %q:\n%s", want, raw)
		}
	}
	env, err := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if err != nil || !strings.Contains(string(env), `CERNE_ACTOR = "agent"`) {
		t.Fatalf("config.toml env fragment missing: %v\n%s", err, env)
	}
	if st := statuses(results)["codex: subagent-context"]; st != StatusSkipped {
		t.Fatalf("subagent degradation not reported: %v", results)
	}

	// Idempotent.
	results, err = Install(dir, ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status != StatusUnchanged && r.Status != StatusSkipped {
			t.Errorf("second install %s = %s, want unchanged/skipped", r.Asset, r.Status)
		}
	}
}

// TestInstallGemini (F-0174 crit. 2/4/5): settings.json hooks block merged
// preserving user keys; .gemini/.env gains CERNE_ACTOR (appended, never
// clobbered); degradations noted.
func TestInstallGemini(t *testing.T) {
	dir := t.TempDir()
	gdir := filepath.Join(dir, ".gemini")
	if err := os.MkdirAll(gdir, 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{"theme":"dark","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo mine"}]}]}}`
	if err := os.WriteFile(filepath.Join(gdir, "settings.json"), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gdir, ".env"), []byte("FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(dir, ProviderGemini); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(gdir, "settings.json"))
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("settings.json invalid: %v\n%s", err, raw)
	}
	if got["theme"] != "dark" || !strings.Contains(string(raw), "echo mine") {
		t.Fatalf("user settings lost:\n%s", raw)
	}
	for _, want := range []string{"BeforeTool", `write_file|replace`, "work-gate --format gemini", "AfterTool", "run_shell_command", "bind-work --format gemini", "session-context --format gemini"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("settings.json missing %q:\n%s", want, raw)
		}
	}
	env, _ := os.ReadFile(filepath.Join(gdir, ".env"))
	if !strings.HasPrefix(string(env), "FOO=bar\n") || !strings.Contains(string(env), "CERNE_ACTOR=agent") {
		t.Fatalf(".env not appended safely:\n%s", env)
	}

	// Idempotent.
	results, err := Install(dir, ProviderGemini)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status != StatusUnchanged && r.Status != StatusSkipped {
			t.Errorf("second install %s = %s, want unchanged/skipped", r.Asset, r.Status)
		}
	}
}

// TestInstallCursor (F-0174 crit. 2/4/5): rules floor + sessionStart context;
// gate/env/subagent reported as degraded (third-party toggle is the gate
// route).
func TestInstallCursor(t *testing.T) {
	dir := t.TempDir()
	results, err := Install(dir, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := os.ReadFile(filepath.Join(dir, ".cursor", "rules", "cerne.mdc"))
	if err != nil {
		t.Fatalf("rules not written: %v", err)
	}
	for _, want := range []string{"alwaysApply: true", "cerne feature new", "cerne work start"} {
		if !strings.Contains(string(rules), want) {
			t.Fatalf("rules missing %q:\n%s", want, rules)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".cursor", "hooks.json"))
	if !strings.Contains(string(raw), "session-context --format cursor") || !strings.Contains(string(raw), `"version"`) {
		t.Fatalf("hooks.json missing sessionStart wiring:\n%s", raw)
	}
	st := statuses(results)
	for _, degraded := range []string{"cursor: work-gate", "cursor: CERNE_ACTOR", "cursor: subagent-context"} {
		if st[degraded] != StatusSkipped {
			t.Fatalf("%s not reported as degraded: %v", degraded, results)
		}
	}

	// Idempotent.
	results, err = Install(dir, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status != StatusUnchanged && r.Status != StatusSkipped {
			t.Errorf("second install %s = %s, want unchanged/skipped", r.Asset, r.Status)
		}
	}
}

// TestInstallUnknownProviderFails: a typo'd provider is an explicit error.
func TestInstallUnknownProviderFails(t *testing.T) {
	if _, err := Install(t.TempDir(), Provider("emacs")); err == nil {
		t.Fatal("want ErrUnknownProvider")
	}
}

// TestDetectProviders: detection by config dir, stable order, none by default.
func TestDetectProviders(t *testing.T) {
	dir := t.TempDir()
	if got := DetectProviders(dir); len(got) != 0 {
		t.Fatalf("empty project detected %v", got)
	}
	for _, d := range []string{".gemini", ".cursor"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := DetectProviders(dir)
	if len(got) != 2 || got[0] != ProviderCursor || got[1] != ProviderGemini {
		t.Fatalf("DetectProviders = %v, want [cursor gemini]", got)
	}
}
