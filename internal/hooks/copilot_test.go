package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDetectCopilot (F-0444 crit. 1): Copilot is detected by its own .github
// files, never by a bare .github/ (every repo has one for Actions).
func TestDetectCopilot(t *testing.T) {
	has := func(dir string) bool {
		for _, p := range DetectProviders(dir) {
			if p == ProviderCopilot {
				return true
			}
		}
		return false
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if has(dir) {
		t.Fatal("a .github/ with only workflows must not detect copilot")
	}
	for _, marker := range []struct {
		rel  string
		file bool
	}{
		{".github/copilot-instructions.md", true},
		{".github/hooks", false},
		{".github/skills", false},
		{".github/agents", false},
	} {
		d := t.TempDir()
		p := filepath.Join(d, filepath.FromSlash(marker.rel))
		if marker.file {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		} else if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if !has(d) {
			t.Fatalf("%s must detect copilot", marker.rel)
		}
	}
	if ConfigDir(ProviderCopilot) != ".github" {
		t.Fatalf("ConfigDir(copilot) = %q", ConfigDir(ProviderCopilot))
	}
}
