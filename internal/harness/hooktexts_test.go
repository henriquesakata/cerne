package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectHookTexts pins F-0340 criterion 6: what the lint reads as "the
// project's hooks".
func TestProjectHookTexts(t *testing.T) {
	proj := t.TempDir()
	home := t.TempDir()
	write := func(root, rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(proj, ".claude/hooks/loose.sh", "LOOSE")
	write(proj, "scripts/hooks/x.mjs", "NODE")
	write(proj, "scripts/y.py", "PY")
	write(proj, "README.md", "NOT-A-HOOK")
	outside := filepath.Join(t.TempDir(), "out.sh")
	if err := os.WriteFile(outside, []byte("OUTSIDE"), 0o644); err != nil {
		t.Fatal(err)
	}
	write(proj, ".claude/settings.json", `{"hooks":{"SessionStart":[{"matcher":"compact","hooks":[
		{"command":"node \"$CLAUDE_PROJECT_DIR/scripts/hooks/x.mjs\""},
		{"command":"python3 scripts/y.py --flag notafile"}]}]}}`)
	write(proj, ".claude/settings.local.json", `{"hooks":{"Stop":[{"hooks":[
		{"command":"echo INLINE-LOCAL README.md.nope `+outside+`"},
		{"command":"node scripts/hooks/x.mjs"}]}]}}`)
	write(home, ".claude/settings.json", `{"hooks":{"Stop":[{"hooks":[{"command":"echo USER-LAYER"}]}]}}`)
	t.Setenv("HOME", home)

	got, err := ProjectHookTexts(proj)
	if err != nil {
		t.Fatal(err)
	}
	texts := map[string]string{}
	for _, h := range got {
		if _, dup := texts[h.Path]; dup {
			t.Errorf("path %s listed twice", h.Path)
		}
		texts[h.Path] = h.Text
	}
	for path, want := range map[string]string{
		".claude/hooks/loose.sh": "LOOSE",
		"scripts/hooks/x.mjs":    "NODE",
		"scripts/y.py":           "PY",
	} {
		if texts[path] != want {
			t.Errorf("%s = %q, want %q", path, texts[path], want)
		}
	}
	var inline, settingsLocator int
	for path, text := range texts {
		if strings.Contains(text, "USER-LAYER") {
			t.Errorf("user layer leaked into %s", path)
		}
		if text == "NOT-A-HOOK" || text == "OUTSIDE" {
			t.Errorf("file that no hook executes was read: %s", path)
		}
		if strings.Contains(text, "INLINE-LOCAL") && strings.HasPrefix(path, ".claude/settings.local.json#hooks.Stop[") {
			inline++
		}
		if strings.HasPrefix(path, ".claude/settings.json#hooks.SessionStart[") {
			settingsLocator++
		}
	}
	if inline != 1 {
		t.Errorf("local inline command listed %d times, want 1", inline)
	}
	if settingsLocator != 2 {
		t.Errorf("project commands listed %d times, want 2", settingsLocator)
	}
}
