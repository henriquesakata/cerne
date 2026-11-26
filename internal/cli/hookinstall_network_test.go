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

const (
	netOpenLine   = "the whole network is open to the agent in this project"
	netNotOpened  = "the network was NOT opened in the project file"
	netBrokenLine = "stay broken"
)

// F-0484 critic #1: the closing line follows what the file holds. The test env
// sets CERNE_CACHE_DIR, so the profile is printed as a snippet (nothing written):
// the output must say the network was NOT opened and point to the snippet.
func TestHookInstallCodexNetworkFlagSnippetPathIsTruthful(t *testing.T) {
	dir := networkProject(t)
	out, err := execCerne(t, "hook", "install", "--dir", dir, "--provider", "codex", "--network")
	if err != nil {
		t.Fatalf("hook install: %v\n%s", err, out)
	}
	if strings.Contains(out, netOpenLine) || strings.Contains(out, netBrokenLine) {
		t.Errorf("false claim on the snippet path:\n%s", out)
	}
	if !strings.Contains(out, netNotOpened) || !strings.Contains(out, "snippet printed above") {
		t.Errorf("output must say the network was not opened and point to the snippet:\n%s", out)
	}
	if !strings.Contains(out, "[permissions.cerne.network]\nenabled = true") {
		t.Errorf("snippet lacks the network table:\n%s", out)
	}
}

// With the profile written (no CERNE_CACHE_DIR), --network says the network is
// open and how to turn it off, and a later install WITHOUT the flag still says
// so, because the file holds the table.
func TestHookInstallCodexNetworkFlagWritten(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := project.SaveConfig(dir, project.Config{ProjectID: "p", Backend: "x"}); err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "hook", "install", "--dir", dir, "--provider", "codex", "--network")
	if err != nil {
		t.Fatalf("hook install: %v\n%s", err, out)
	}
	for _, want := range []string{netOpenLine, "enabled = false"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, netBrokenLine) || strings.Contains(out, netNotOpened) {
		t.Errorf("contradictory line:\n%s", out)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if !strings.Contains(string(raw), "[permissions.cerne.network]\nenabled = true") {
		t.Errorf("network table missing:\n%s", raw)
	}
	out, err = execCerne(t, "hook", "install", "--dir", dir, "--provider", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, netOpenLine) || strings.Contains(out, netBrokenLine) {
		t.Errorf("install without --network on a profile with the table:\n%s", out)
	}
}

// F-0484 critic #9: --network with the default provider list is refused before
// any write when codex is not detected, and accepted when .codex/ exists.
func TestHookInstallNetworkDefaultProviders(t *testing.T) {
	dir := networkProject(t)
	out, err := execCerne(t, "hook", "install", "--dir", dir, "--network")
	if !errors.Is(err, ErrNetworkNeedsSandboxProvider) {
		t.Fatalf("err = %v, want ErrNetworkNeedsSandboxProvider\n%s", err, out)
	}
	if ents, _ := os.ReadDir(dir); len(ents) != 1 || ents[0].Name() != ".cernerc" {
		t.Errorf("project dir changed on a refused install: %v", ents)
	}
	if err := os.Mkdir(filepath.Join(dir, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerne(t, "hook", "install", "--dir", dir, "--network"); err != nil {
		t.Fatalf("codex detected but refused: %v\n%s", err, out)
	}
}

// F-0484 crit. 2: --network without codex among the providers is refused
// before any write.
func TestHookInstallNetworkRefusedWithoutCodex(t *testing.T) {
	dir := networkProject(t)
	out, err := execCerne(t, "hook", "install", "--dir", dir, "--provider", "gemini", "--network")
	if !errors.Is(err, ErrNetworkNeedsSandboxProvider) {
		t.Fatalf("err = %v, want ErrNetworkNeedsSandboxProvider\n%s", err, out)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 || ents[0].Name() != ".cernerc" {
		t.Errorf("project dir changed on a refused install: %v", ents)
	}
}
