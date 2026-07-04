package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectHeadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	const sha = "1a2b3c4d5e6f70819293a4b5c6d7e8f901234567"
	if err := SaveProjectHead(dir, sha); err != nil {
		t.Fatalf("SaveProjectHead: %v", err)
	}
	got, err := LoadProjectHead(dir)
	if err != nil {
		t.Fatalf("LoadProjectHead: %v", err)
	}
	if got != sha {
		t.Fatalf("LoadProjectHead = %q, want %q", got, sha)
	}
	// One line, newline-terminated, under the gitignored .docs/.
	raw, err := os.ReadFile(filepath.Join(dir, ".docs", ".projecthead"))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if string(raw) != sha+"\n" {
		t.Fatalf("sidecar content = %q, want %q", raw, sha+"\n")
	}
}

func TestLoadProjectHeadAbsent(t *testing.T) {
	_, err := LoadProjectHead(t.TempDir())
	if !errors.Is(err, ErrNoProjectHead) {
		t.Fatalf("LoadProjectHead err = %v, want ErrNoProjectHead", err)
	}
}
