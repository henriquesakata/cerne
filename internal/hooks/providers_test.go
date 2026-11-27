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
	for _, want := range []string{"SessionStart", "PreToolUse", `Edit|Write`, "cerne hook work-gate", "cerne hook bind-work", "cerne hook provenance", "cerne hook stop-check", "session-context --format codex"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("hooks.json missing %q:\n%s", want, raw)
		}
	}
	// F-0426: the retired lifecycle reminder is no longer wired.
	if strings.Contains(string(raw), "lifecycle-reminder") {
		t.Fatalf("hooks.json still wires the retired lifecycle reminder:\n%s", raw)
	}
	env, err := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if err != nil || !strings.Contains(string(env), `CERNE_ACTOR = "agent"`) {
		t.Fatalf("config.toml env fragment missing: %v\n%s", err, env)
	}
	// F-0375: the provider of documents written from Codex, declared; no
	// model — Codex does not expose it to the shell.
	if !strings.Contains(string(env), `CERNE_PROVIDER = "openai"`) || strings.Contains(string(env), "CERNE_MODEL") {
		t.Fatalf("config.toml must declare CERNE_PROVIDER and no CERNE_MODEL:\n%s", env)
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
	if !strings.Contains(string(env), "CERNE_PROVIDER=google\n") || strings.Contains(string(env), "CERNE_MODEL") {
		t.Fatalf(".env must declare CERNE_PROVIDER (F-0375) and no CERNE_MODEL:\n%s", env)
	}
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

// TestInstallCursor (F-0174 crit. 2/4/5, F-0527 crit. 1): rules floor +
// full standalone hook set; CERNE_ACTOR/subagent still degraded because
// they need no installation, not because they are missing.
func TestInstallCursor(t *testing.T) {
	// F-0527 crit. 1
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
	// Full standalone hook set — no longer just sessionStart.
	for _, want := range []string{
		"session-context --format cursor",
		`"version"`,
		"work-gate",
		"tool-notes",
		"bind-work",
		"provenance",
		"tool-notes --after --format cursor",
		"precompact",
		"stop-check",
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("hooks.json missing %q:\n%s", want, raw)
		}
	}
	// cursor: work-gate is no longer skipped — it's natively installed.
	st := statuses(results)
	if _, hasWorkGate := st["cursor: work-gate"]; hasWorkGate {
		t.Fatalf("cursor: work-gate should not appear as a separate result (it's now native): %v", results)
	}
	for _, degraded := range []string{"cursor: CERNE_ACTOR", "cursor: subagent-context"} {
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

// TestInstallCursorFullHooks (F-0527 crit. 1): all events are written to
// .cursor/hooks.json with the right matchers, third-party entries are
// preserved, and upgrading old cerne entries never duplicates them.
func TestInstallCursorFullHooks(t *testing.T) {
	// F-0527 crit. 1
	dir := t.TempDir()

	// Pre-populate with a third-party entry and an old cerne entry
	// (old format: session-context without --format cursor).
	cursorDir := filepath.Join(dir, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Use a JSON-safe old cerne command (simulating a pre-F-0527 install
	// that just had "cerne hook session-context" without --format cursor).
	oldHooks := `{"version":1,"hooks":{"sessionStart":[{"command":"third-party-hook"},{"command":"cerne hook session-context"}],"postToolUse":[{"matcher":"Shell","command":"user-post-hook"}]}}`
	if err := os.WriteFile(filepath.Join(cursorDir, "hooks.json"), []byte(oldHooks), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(dir, ProviderCursor); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(cursorDir, "hooks.json"))
	if err != nil {
		t.Fatalf("hooks.json not written: %v", err)
	}
	content := string(raw)

	// All cerne events present.
	for _, want := range []string{
		"cerne hook session-context --format cursor",
		"cerne hook work-gate",
		"cerne hook tool-notes",
		"cerne hook bind-work",
		"cerne hook provenance",
		"cerne hook tool-notes --after --format cursor",
		"cerne hook precompact",
		"cerne hook stop-check",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("hooks.json missing %q:\n%s", want, content)
		}
	}

	// Third-party entries preserved.
	if !strings.Contains(content, "third-party-hook") {
		t.Fatalf("third-party sessionStart hook lost:\n%s", content)
	}
	if !strings.Contains(content, "user-post-hook") {
		t.Fatalf("third-party postToolUse hook lost:\n%s", content)
	}

	// Old cerne entry replaced, not duplicated: "session-context" appears
	// exactly once (the new --format cursor one; the old one is gone).
	occurrences := strings.Count(content, "cerne hook session-context")
	if occurrences != 1 {
		t.Fatalf("cerne hook session-context duplicated (%d occurrences):\n%s", occurrences, content)
	}

	// Matchers present.
	if !strings.Contains(content, `"Write"`) {
		t.Fatalf("preToolUse Write matcher missing:\n%s", content)
	}
	if !strings.Contains(content, `"Shell"`) {
		t.Fatalf("Shell matcher missing:\n%s", content)
	}
	if !strings.Contains(content, `"Shell|Write"`) {
		t.Fatalf("Shell|Write matcher missing:\n%s", content)
	}

	// Idempotent: second install reports unchanged.
	results, err := Install(dir, ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Asset == ".cursor/hooks.json" && r.Status != StatusUnchanged {
			t.Errorf("second install hooks.json = %s, want unchanged", r.Status)
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

// TestInstallCodexHearsTheChannelAtPrompt (F-0491 crit. 8): Codex documents
// additionalContext on UserPromptSubmit and SessionStart (R-0035), so the
// install wires the prompt hook that carries the channel, as on Claude Code;
// the session start already carries it (session-context).
func TestInstallCodexHearsTheChannelAtPrompt(t *testing.T) {
	dir := t.TempDir()
	results, err := Install(dir, ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".codex", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	wired := func(event, needle string) bool {
		for _, g := range cfg.Hooks[event] {
			for _, h := range g.Hooks {
				if strings.Contains(h.Command, needle) {
					return true
				}
			}
		}
		return false
	}
	if !wired("UserPromptSubmit", "cerne hook prompt-notes") || !wired("SessionStart", "cerne hook session-context --format codex") {
		t.Fatalf("codex hooks.json lacks the channel at prompt submit or session start:\n%s", raw)
	}
	if _, skipped := statuses(results)["codex: prompt-notes"]; skipped {
		t.Fatalf("prompt-notes is wired now: no skipped line for it: %v", results)
	}
}
