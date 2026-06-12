package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pins the loud-failure contract when the 4-digit ID space is exhausted.
func TestNextIDExhausted(t *testing.T) {
	dir := t.TempDir()
	featDir := filepath.Join(dir, "entities", "features")
	if err := os.MkdirAll(featDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(featDir, "F-9999.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NextID(dir, "F")
	if err == nil || !strings.Contains(err.Error(), "exhausted") {
		t.Fatalf("want exhaustion error, got %v", err)
	}
}

// Pins the contract: a malformed NDJSON line is an error, never skipped.
func TestReadEventsMalformedLine(t *testing.T) {
	dir := t.TempDir()
	evDir := filepath.Join(dir, "events")
	if err := os.MkdirAll(evDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evDir, "2026-01.ndjson"), []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvents(dir); err == nil {
		t.Fatal("want error for malformed line, got nil")
	}
}
