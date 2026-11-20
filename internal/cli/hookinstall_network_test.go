package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

func networkProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := project.SaveConfig(dir, project.Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// F-0484 crit. 1: without --network nothing opens the network, and the output
// says what stays broken inside Codex and shows the command.
func TestHookInstallCodexWithoutNetworkSaysWhatBreaks(t *testing.T) {
	dir := networkProject(t)
	out, err := execCerne(t, "hook", "install", "--dir", dir, "--provider", "codex")
	if err != nil {
		t.Fatalf("hook install: %v\n%s", err, out)
	}
	for _, want := range []string{"pull and push with a remote backend", "AI layer", "cerne hook install --provider codex --network"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if strings.Contains(string(raw), "network") || strings.Contains(out, "[permissions.cerne.network]") {
		t.Errorf("network table written without --network:\n%s", raw)
	}
}

// F-0484 crit. 2: with --network the table is written and the output says the
// whole network is open and how to turn it off.
func TestHookInstallCodexNetworkFlag(t *testing.T) {
	dir := networkProject(t)
	out, err := execCerne(t, "hook", "install", "--dir", dir, "--provider", "codex", "--network")
	if err != nil {
		t.Fatalf("hook install: %v\n%s", err, out)
	}
	for _, want := range []string{"the whole network is open to the agent in this project", "enabled = false"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "stay broken") {
		t.Errorf("warning about broken network printed with --network:\n%s", out)
	}
	// The test env sets CERNE_CACHE_DIR, so the profile is printed as a snippet
	// (machine-specific folder) instead of written; either way it carries the table.
	raw, _ := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if !strings.Contains(out+string(raw), "[permissions.cerne.network]\nenabled = true") {
		t.Errorf("network table missing:\n%s\n%s", out, raw)
	}
}

// F-0484 crit. 2: --network without codex among the providers is refused
// before any write.
func TestHookInstallNetworkRefusedWithoutCodex(t *testing.T) {
	dir := networkProject(t)
	out, err := execCerne(t, "hook", "install", "--dir", dir, "--provider", "gemini", "--network")
	if !errors.Is(err, ErrNetworkNeedsCodex) {
		t.Fatalf("err = %v, want ErrNetworkNeedsCodex\n%s", err, out)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != ".cernerc" {
		t.Errorf("project dir changed on a refused install: %v", ents)
	}
}
