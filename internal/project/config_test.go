package project

import (
	"errors"
	"os"
	"path/filepath"
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
