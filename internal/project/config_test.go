package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Config{
		ProjectID:    "cerne",
		Backend:      "https://gitlab.com/rockt/cerne-docs.git",
		DefaultScope: "core",
	}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, foundDir, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got != want {
		t.Fatalf("LoadConfig = %+v, want %+v", got, want)
	}
	if foundDir != dir {
		t.Fatalf("LoadConfig dir = %q, want %q", foundDir, dir)
	}
}

func TestLoadConfigWalksUpFromNestedDir(t *testing.T) {
	root := t.TempDir()
	want := Config{ProjectID: "nested", Backend: "/tmp/backend"}
	if err := SaveConfig(root, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	got, foundDir, err := LoadConfig(nested)
	if err != nil {
		t.Fatalf("LoadConfig from nested: %v", err)
	}
	if got != want {
		t.Fatalf("LoadConfig = %+v, want %+v", got, want)
	}
	if foundDir != root {
		t.Fatalf("LoadConfig dir = %q, want %q", foundDir, root)
	}
}

func TestLoadConfigNoProject(t *testing.T) {
	_, _, err := LoadConfig(t.TempDir())
	if !errors.Is(err, ErrNoProject) {
		t.Fatalf("LoadConfig err = %v, want ErrNoProject", err)
	}
}

// TestConfigOKFRoundTrip pins the .cernerc `okf` key: it persists and reloads
// when true, and is omitted (omitempty) when false.
func TestConfigOKFRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x", OKF: true}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if !strings.Contains(string(raw), "okf = true") {
		t.Fatalf(".cernerc missing okf key:\n%s", raw)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !got.OKF {
		t.Fatalf("loaded OKF = false, want true")
	}

	// omitempty: a false OKF is not written to .cernerc.
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x"}); err != nil {
		t.Fatalf("SaveConfig off: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if strings.Contains(string(raw), "okf") {
		t.Fatalf("okf=false should be omitted:\n%s", raw)
	}
}

// TestConfigAutocommitLockRoundTrip pins the .cernerc `autocommit_lock` key
// (F-0060): persists and reloads when true, omitted (omitempty) when false.
func TestConfigAutocommitLockRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x", AutocommitLock: true}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if !strings.Contains(string(raw), "autocommit_lock = true") {
		t.Fatalf(".cernerc missing autocommit_lock key:\n%s", raw)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !got.AutocommitLock {
		t.Fatalf("loaded AutocommitLock = false, want true")
	}

	// omitempty: a false AutocommitLock is not written to .cernerc.
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x"}); err != nil {
		t.Fatalf("SaveConfig off: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if strings.Contains(string(raw), "autocommit_lock") {
		t.Fatalf("autocommit_lock=false should be omitted:\n%s", raw)
	}
}
