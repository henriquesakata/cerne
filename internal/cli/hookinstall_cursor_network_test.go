package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// F-0512 crit. 2: --network is accepted for cursor (explicit or detected) and
// allows only the remote backend's host in .cursor/sandbox.json; with neither
// codex nor cursor it is still refused, naming both.
func TestHookInstallCursorNetwork(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := project.SaveConfig(dir, project.Config{ProjectID: "p", Backend: "git@git.example.com:o/r.git"}); err != nil {
		t.Fatal(err)
	}

	out, err := execCerne(t, "hook", "install", "--dir", dir, "--provider", "gemini", "--network")
	if !errors.Is(err, ErrNetworkNeedsSandboxProvider) || !strings.Contains(err.Error(), "cursor") {
		t.Fatalf("err = %v, want ErrNetworkNeedsSandboxProvider naming cursor\n%s", err, out)
	}

	out, err = execCerne(t, "hook", "install", "--dir", dir, "--provider", "cursor", "--network")
	if err != nil {
		t.Fatalf("hook install --provider cursor --network: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".cursor", "sandbox.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"git.example.com"`) || strings.Contains(string(raw), `"default"`) {
		t.Errorf("sandbox.json does not allow only the backend host:\n%s", raw)
	}
	if strings.Contains(out, "codex:") {
		t.Errorf("Codex lines printed for a cursor install:\n%s", out)
	}
	for _, want := range []string{"cursor: sandbox.json: created", "cursor: mcp.json: created"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// Detected by its folder, cursor makes the default provider list accept it.
	if out, err := execCerne(t, "hook", "install", "--dir", dir, "--network"); err != nil {
		t.Fatalf("cursor detected but --network refused: %v\n%s", err, out)
	}
}
