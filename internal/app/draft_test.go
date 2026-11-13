package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFeatureDoc(t *testing.T, repoDir, id, name, body string) {
	t.Helper()
	dir := filepath.Join(repoDir, "content", "features", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestDriftDraftPrompt(t *testing.T) {
	repo := t.TempDir()
	writeFeatureDoc(t, repo, "F-0001", "result.md", "# Motor de core\n\nEntregou o parser antigo XYZ.\n")

	got, err := DriftDraftPrompt(repo, "F-0001", "abcdef1234567890", []string{"internal/core/core.go", "internal/core/parser.go"})
	if err != nil {
		t.Fatalf("DriftDraftPrompt: %v", err)
	}
	for _, want := range []string{
		"internal/core/core.go",   // changed path (crit. 6)
		"internal/core/parser.go", // changed path
		"abcdef1",                 // short baseline (crit. 6)
		"parser antigo XYZ",       // current doc content (crit. 6)
		"cerne feature errata F-0001 --file .docs/.drift-proposals/F-0001.md", // apply cmd (crit. 5)
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q:\n%s", want, got)
		}
	}
}

func TestDriftDraftPromptFallsBackToSpec(t *testing.T) {
	repo := t.TempDir()
	writeFeatureDoc(t, repo, "F-0002", "spec.md", "# Spec só\n\nApenas a spec existe.\n")
	got, err := DriftDraftPrompt(repo, "F-0002", "deadbeef", []string{"a.go"})
	if err != nil {
		t.Fatalf("DriftDraftPrompt: %v", err)
	}
	if !strings.Contains(got, "Apenas a spec existe") {
		t.Fatalf("prompt should fall back to spec.md:\n%s", got)
	}
}

func TestDriftDraftPromptNoDoc(t *testing.T) {
	repo := t.TempDir()
	if _, err := DriftDraftPrompt(repo, "F-0003", "abc", []string{"a.go"}); !errors.Is(err, ErrNoEntityDoc) {
		t.Fatalf("err = %v, want ErrNoEntityDoc", err)
	}
}
