package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

type codexNetCfg struct {
	Permissions map[string]struct {
		Network map[string]any `toml:"network"`
	} `toml:"permissions"`
}

func codexNetEnabled(t *testing.T, raw string) (enabled, present bool) {
	t.Helper()
	var c codexNetCfg
	if _, err := toml.Decode(raw, &c); err != nil {
		t.Fatalf("not valid TOML: %v\n%s", err, raw)
	}
	n, ok := c.Permissions["cerne"]
	if !ok || n.Network == nil {
		return false, false
	}
	e, _ := n.Network["enabled"].(bool)
	return e, true
}

// F-0484 crit. 2: --network writes [permissions.cerne.network] enabled = true.
func TestInstallCodexSandboxNetworkOnRequest(t *testing.T) {
	proj, _ := codexProject(t, "x")
	if _, err := InstallNetwork(proj, true, ProviderCodex); err != nil {
		t.Fatal(err)
	}
	_, raw := readCodexCfg(t, proj)
	if en, ok := codexNetEnabled(t, raw); !ok || !en {
		t.Fatalf("network table missing or not enabled:\n%s", raw)
	}
}

// F-0484 crit. 1: without the request nothing network-related is written.
func TestInstallCodexSandboxNoNetworkByDefault(t *testing.T) {
	proj, _ := codexProject(t, "x")
	if _, err := Install(proj, ProviderCodex); err != nil {
		t.Fatal(err)
	}
	_, raw := readCodexCfg(t, proj)
	if strings.Contains(raw, "network") {
		t.Fatalf("network written without the request:\n%s", raw)
	}
}

// F-0484 crit. 2: a file with the F-0474 block and no table gets only the
// table; user lines stay; a second run is byte-identical.
func TestInstallCodexSandboxNetworkOnExistingBlock(t *testing.T) {
	proj, _ := codexProject(t, "x")
	if _, err := Install(proj, ProviderCodex); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(proj, ".codex", "config.toml")
	raw, _ := os.ReadFile(path)
	user := string(raw) + "\n[mcp_servers.foo]\ncommand = \"foo\"\n"
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallNetwork(proj, true, ProviderCodex); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if en, ok := codexNetEnabled(t, string(got)); !ok || !en {
		t.Fatalf("network table not added to the existing block:\n%s", got)
	}
	if !strings.Contains(string(got), "[mcp_servers.foo]\ncommand = \"foo\"") {
		t.Errorf("user lines lost:\n%s", got)
	}
	if _, err := InstallNetwork(proj, true, ProviderCodex); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if string(again) != string(got) {
		t.Errorf("second run changed the file:\n%s\n---\n%s", got, again)
	}
	// an install without the request keeps the table
	if _, err := Install(proj, ProviderCodex); err != nil {
		t.Fatal(err)
	}
	kept, _ := os.ReadFile(path)
	if string(kept) != string(got) {
		t.Errorf("install without --network changed the file:\n%s", kept)
	}
}

// F-0484 crit. 3: the printed snippet carries the table only with the request.
func TestCodexSandboxSnippetNetworkOnlyOnRequest(t *testing.T) {
	paths := []string{"~/x"}
	if s := codexSandboxSnippet(paths, false); strings.Contains(s, "network") {
		t.Errorf("snippet has network without the request:\n%s", s)
	}
	s := codexSandboxSnippet(paths, true)
	i := strings.Index(s, "default_permissions")
	if en, ok := codexNetEnabled(t, s[i:]); !ok || !en {
		t.Errorf("snippet missing network table:\n%s", s)
	}
}

// F-0484 crit. 3: a machine-specific folder prints the snippet with the table.
func TestInstallCodexSandboxMachineSnippetNetwork(t *testing.T) {
	proj, _ := codexProject(t, "x")
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	off, _ := InstallNetwork(proj, false, ProviderCodex)
	on, _ := InstallNetwork(proj, true, ProviderCodex)
	find := func(rs []Result) string {
		for _, r := range rs {
			if r.Asset == codexSandboxAsset {
				return r.Note
			}
		}
		return ""
	}
	if strings.Contains(find(off), "network") || !strings.Contains(find(on), "permissions.cerne.network") {
		t.Errorf("machine snippet network mismatch:\noff: %s\non: %s", find(off), find(on))
	}
}
