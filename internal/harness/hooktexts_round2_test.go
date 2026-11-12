package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProj(t *testing.T, root, rel string, body []byte) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// Round 2 item D: binaries and huge files are not hook text.
func TestProjectHookTextsSkipsBinaryAndHuge(t *testing.T) {
	proj := t.TempDir()
	writeProj(t, proj, "bin/tool", []byte("MZ\x00\x01 N-0600 regra 15"))
	writeProj(t, proj, "big.js", []byte(strings.Repeat("x", 1<<20+1)))
	writeProj(t, proj, "ok.js", []byte("fine"))
	writeProj(t, proj, ".claude/settings.json", []byte(`{"hooks":{"Stop":[{"hooks":[{"command":"node bin/tool big.js ok.js"}]}]}}`))
	got, err := ProjectHookTexts(proj)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, h := range got {
		paths[h.Path] = true
	}
	if paths["bin/tool"] || paths["big.js"] {
		t.Errorf("binary or >1MB file was read: %v", paths)
	}
	if !paths["ok.js"] {
		t.Errorf("regular file skipped: %v", paths)
	}
}

// Round 2 item E: the locator is the real position in settings.json.
func TestProjectHookTextsRealLocator(t *testing.T) {
	proj := t.TempDir()
	writeProj(t, proj, ".claude/settings.json", []byte(`{"hooks":{"Stop":[
		{"matcher":"a","hooks":[{"command":"echo A0"}]},
		{"matcher":"b","hooks":[{"command":"echo B0"},{"command":"echo B1"}]}]}}`))
	got, err := ProjectHookTexts(proj)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		".claude/settings.json#hooks.Stop[0].hooks[0]": "echo A0",
		".claude/settings.json#hooks.Stop[1].hooks[0]": "echo B0",
		".claude/settings.json#hooks.Stop[1].hooks[1]": "echo B1",
	}
	for _, h := range got {
		if want[h.Path] != h.Text {
			t.Errorf("unexpected %s = %q", h.Path, h.Text)
		}
		delete(want, h.Path)
	}
	if len(want) != 0 {
		t.Errorf("missing locators: %v", want)
	}
}
