package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	const sha = "740e46f43f30cc2436e4d2d190c90b0dba4a8b28"
	if err := SaveSessionSnapshot(dir, sha); err != nil {
		t.Fatalf("SaveSessionSnapshot: %v", err)
	}
	got, err := LoadSessionSnapshot(dir)
	if err != nil {
		t.Fatalf("LoadSessionSnapshot: %v", err)
	}
	if got != sha {
		t.Fatalf("LoadSessionSnapshot = %q, want %q", got, sha)
	}
	// One line, newline-terminated, under the gitignored .docs/.
	raw, err := os.ReadFile(filepath.Join(dir, ".docs", ".snapshot"))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if string(raw) != sha+"\n" {
		t.Fatalf("sidecar content = %q, want %q", raw, sha+"\n")
	}
}

func TestLoadSessionSnapshotAbsent(t *testing.T) {
	_, err := LoadSessionSnapshot(t.TempDir())
	if !errors.Is(err, ErrNoSessionSnapshot) {
		t.Fatalf("LoadSessionSnapshot err = %v, want ErrNoSessionSnapshot", err)
	}
}

func TestSaveSessionSnapshotOverwrites(t *testing.T) {
	dir := t.TempDir()
	if err := SaveSessionSnapshot(dir, "aaaa"); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := SaveSessionSnapshot(dir, "bbbb"); err != nil {
		t.Fatalf("second save: %v", err)
	}
	got, err := LoadSessionSnapshot(dir)
	if err != nil {
		t.Fatalf("LoadSessionSnapshot: %v", err)
	}
	if got != "bbbb" {
		t.Fatalf("LoadSessionSnapshot = %q, want %q", got, "bbbb")
	}
}
