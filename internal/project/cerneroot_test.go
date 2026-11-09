package project

import (
	"errors"
	"strings"
	"testing"
)

// F-0876 crit. 1: CERNE_ROOT points at the project when the upward walk from
// the working directory finds no .cernerc.
func TestLoadConfigCerneRootFallback(t *testing.T) {
	root := t.TempDir()
	if err := SaveConfig(root, Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	outside := t.TempDir() // no .cernerc above it

	t.Setenv("CERNE_ROOT", root)
	cfg, dir, err := LoadConfig(outside)
	if err != nil {
		t.Fatalf("LoadConfig with CERNE_ROOT: %v", err)
	}
	if cfg.ProjectID != "p" || dir != root {
		t.Fatalf("got project %q dir %q, want p %q", cfg.ProjectID, dir, root)
	}

	// The upward walk wins when it finds a project.
	other := t.TempDir()
	if err := SaveConfig(other, Config{ProjectID: "q", Backend: "/tmp/b"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if cfg, _, err := LoadConfig(other); err != nil || cfg.ProjectID != "q" {
		t.Fatalf("walk must win over CERNE_ROOT: %q, %v", cfg.ProjectID, err)
	}

	// CERNE_ROOT without a .cernerc is an error that names the variable.
	t.Setenv("CERNE_ROOT", t.TempDir())
	_, _, err = LoadConfig(outside)
	if !errors.Is(err, ErrNoProject) || !strings.Contains(err.Error(), "CERNE_ROOT") {
		t.Fatalf("want ErrNoProject naming CERNE_ROOT, got %v", err)
	}
}
