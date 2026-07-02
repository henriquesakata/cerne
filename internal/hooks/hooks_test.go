package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstall_CreatesWhenAbsent (C7): with no .claude/, install writes the
// script and a fresh settings.json wiring the hook; both report created.
func TestInstall_CreatesWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	results, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "hooks", hookScript)); err != nil {
		t.Fatalf("script not installed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), hookScript) {
		t.Fatalf("settings.json missing hook wiring:\n%s", b)
	}
	if !isValidJSON(b) {
		t.Fatalf("settings.json is not valid JSON:\n%s", b)
	}
	for _, r := range results {
		if r.Status != StatusCreated {
			t.Errorf("%s: status=%s, want created", r.Asset, r.Status)
		}
	}
}

// TestInstall_MergesPreservingExisting (C7): a user's existing hooks and other
// top-level keys survive the merge; our PostToolUse entry is added.
func TestInstall_MergesPreservingExisting(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "SessionStart": [{"hooks":[{"type":"command","command":"echo hi"}]}],
    "PreToolUse": [{"matcher":"Write","hooks":[{"type":"command","command":"echo w"}]}]
  }
}`
	if err := os.WriteFile(filepath.Join(claude, "settings.json"), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(filepath.Join(claude, "settings.json"))
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("merged settings.json invalid: %v\n%s", err, b)
	}
	if _, ok := got["permissions"]; !ok {
		t.Errorf("top-level 'permissions' key lost:\n%s", b)
	}
	hooks, _ := got["hooks"].(map[string]any)
	if _, ok := hooks["SessionStart"]; !ok {
		t.Errorf("existing SessionStart hook lost:\n%s", b)
	}
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Errorf("existing PreToolUse hook lost:\n%s", b)
	}
	if !strings.Contains(string(b), hookScript) {
		t.Errorf("our hook not added:\n%s", b)
	}
	// preserved the user's command payloads
	if !strings.Contains(string(b), "echo hi") || !strings.Contains(string(b), "echo w") {
		t.Errorf("user hook commands lost:\n%s", b)
	}
}

// TestInstall_Idempotent (C7): a second install changes nothing and never
// duplicates the wiring.
func TestInstall_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	results, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status != StatusUnchanged {
			t.Errorf("second install %s: status=%s, want unchanged", r.Asset, r.Status)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if n := strings.Count(string(b), hookScript); n != 1 {
		t.Errorf("hook wired %d times, want 1:\n%s", n, b)
	}
}

// TestInstall_FailsOnCorruptJSON (C7): a corrupt settings.json is fail-closed —
// install errors and leaves the file untouched.
func TestInstall_FailsOnCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("{ this is not json ")
	sp := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(sp, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir); err == nil {
		t.Fatalf("expected error on corrupt settings.json")
	}
	b, _ := os.ReadFile(sp)
	if string(b) != string(corrupt) {
		t.Errorf("corrupt settings.json was modified (not fail-closed):\n%s", b)
	}
}

func isValidJSON(b []byte) bool {
	var v any
	return json.Unmarshal(b, &v) == nil
}
