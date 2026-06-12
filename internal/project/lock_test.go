package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLockRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Lock{
		Version:  1,
		Backend:  "https://gitlab.com/rockt/cerne-docs.git",
		Snapshot: "0123456789abcdef0123456789abcdef01234567",
		Materialized: map[string]string{
			"features/F-0001.md":  "aa11",
			"decisions/D-0001.md": "bb22",
		},
	}
	if err := SaveLock(dir, want); err != nil {
		t.Fatalf("SaveLock: %v", err)
	}
	got, err := LoadLock(dir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadLock = %+v, want %+v", got, want)
	}
}

func TestLoadLockMissing(t *testing.T) {
	_, err := LoadLock(t.TempDir())
	if !errors.Is(err, ErrNoLock) {
		t.Fatalf("LoadLock err = %v, want ErrNoLock", err)
	}
}

func TestLockFileFormat(t *testing.T) {
	dir := t.TempDir()
	l := Lock{
		Version:      1,
		Backend:      "/tmp/backend",
		Snapshot:     "deadbeef",
		Materialized: map[string]string{"features/F-0001.md": "aa11"},
	}
	if err := SaveLock(dir, l); err != nil {
		t.Fatalf("SaveLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "docs.lock"))
	if err != nil {
		t.Fatalf("read docs.lock: %v", err)
	}
	want, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	want = append(want, '\n')
	if !bytes.Equal(data, want) {
		t.Fatalf("docs.lock = %q, want pretty JSON (2-space indent, trailing newline) %q", data, want)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Fatalf("docs.lock missing trailing newline")
	}
	if !bytes.Contains(data, []byte("\n  \"version\"")) {
		t.Fatalf("docs.lock not 2-space indented: %q", data)
	}
}
