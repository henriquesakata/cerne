package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// codexProject builds a fixture project (with .cernerc) and a fixture HOME.
func codexProject(t *testing.T, backend string) (proj, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CERNE_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	proj = t.TempDir()
	rc := "project_id = \"demo\"\nbackend = \"" + backend + "\"\n"
	if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte(rc), 0o644); err != nil {
		t.Fatal(err)
	}
	return proj, home
}

type codexCfg struct {
	DefaultPermissions string `toml:"default_permissions"`
	Permissions        map[string]struct {
		Extends    string            `toml:"extends"`
		Filesystem map[string]string `toml:"filesystem"`
	} `toml:"permissions"`
}

func readCodexCfg(t *testing.T, proj string) (codexCfg, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(proj, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var c codexCfg
	if _, err := toml.Decode(string(raw), &c); err != nil {
		t.Fatalf("config.toml is not valid TOML: %v\n%s", err, raw)
	}
	return c, string(raw)
}

// F-0474 crit. 1: the profile opens the cache (both OS forms) and the local
// backend, with ~/ paths, and default_permissions is a top-level key.
func TestInstallCodexSandboxBlock(t *testing.T) {
	proj, home := codexProject(t, "x")
	backend := filepath.Join(home, "Library", "Application Support", "cerne", "backends", "demo.git")
	bareRepo(t, backend) // F-0512 review #1b: only a repository is opened
	if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte("project_id = \"demo\"\nbackend = \""+backend+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(proj, ProviderCodex); err != nil {
		t.Fatal(err)
	}
	c, raw := readCodexCfg(t, proj)
	if c.DefaultPermissions != "cerne" {
		t.Fatalf("default_permissions not a top-level key:\n%s", raw)
	}
	p := c.Permissions["cerne"]
	if p.Extends != ":workspace" {
		t.Fatalf("extends = %q", p.Extends)
	}
	for _, want := range []string{"~/Library/Caches/cerne/demo", "~/.cache/cerne/demo", "~/Library/Application Support/cerne/backends/demo.git"} {
		if p.Filesystem[want] != "write" {
			t.Errorf("filesystem missing %q: %v", want, p.Filesystem)
		}
	}
	if !strings.Contains(raw, `CERNE_ACTOR = "agent"`) {
		t.Errorf("env fragment lost:\n%s", raw)
	}
}

// Crit. 2: user lines survive, a second run changes nothing, nothing is
// written outside the project.
func TestInstallCodexSandboxPreservesAndIsIdempotent(t *testing.T) {
	proj, home := codexProject(t, "git@example.com:o/r.git")
	path := filepath.Join(proj, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	user := "model = \"gpt-x\"\n\n[mcp_servers.foo]\ncommand = \"foo\"\n"
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(proj, ProviderCodex); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	for _, l := range []string{`model = "gpt-x"`, "[mcp_servers.foo]", `command = "foo"`} {
		if !strings.Contains(string(first), l) {
			t.Errorf("user line %q lost:\n%s", l, first)
		}
	}
	c, _ := readCodexCfg(t, proj)
	if c.DefaultPermissions != "cerne" {
		t.Fatalf("default_permissions missing:\n%s", first)
	}
	if _, err := Install(proj, ProviderCodex); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatalf("second run changed the file:\n%s\n---\n%s", first, second)
	}
	if ents, _ := os.ReadDir(home); len(ents) != 0 {
		t.Errorf("home was written: %v", ents)
	}
}

func TestInstallCodexSandboxKeepsUserDefaultPermissions(t *testing.T) {
	proj, _ := codexProject(t, "git@example.com:o/r.git")
	path := filepath.Join(proj, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("default_permissions = \":danger-no-sandbox\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := Install(proj, ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "[permissions.cerne") || strings.Count(string(raw), "default_permissions") != 1 {
		t.Fatalf("user default_permissions overwritten:\n%s", raw)
	}
	found := false
	for _, r := range results {
		if r.Asset == "codex: config.toml (sandbox)" {
			found = true
			if r.Status != StatusSkipped || !strings.Contains(r.Note, "[permissions.cerne]") {
				t.Errorf("want skipped with the snippet, got %+v", r)
			}
		}
	}
	if !found {
		t.Error("no sandbox result")
	}
}

func TestInstallCodexSandboxInvalidTOMLWritesNothing(t *testing.T) {
	proj, _ := codexProject(t, "git@example.com:o/r.git")
	path := filepath.Join(proj, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := "this is = = not toml\n"
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(proj, ProviderCodex); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != bad {
		t.Fatalf("invalid file was modified:\n%s", raw)
	}
}

// A machine-specific folder is printed with the --add-dir alternative, never
// written to the versioned file.
func TestInstallCodexSandboxMachineSpecificIsPrinted(t *testing.T) {
	proj, _ := codexProject(t, "git@example.com:o/r.git")
	cache := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cache)

	results, err := Install(proj, ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(proj, ".codex", "config.toml"))
	if strings.Contains(string(raw), cache) || strings.Contains(string(raw), "[permissions.cerne") {
		t.Fatalf("machine path written to the project file:\n%s", raw)
	}
	var note string
	for _, r := range results {
		if strings.HasPrefix(r.Asset, "codex: config.toml (sandbox") {
			note += r.Note
		}
	}
	for _, want := range []string{filepath.Join(cache, "demo"), "codex --add-dir", "[permissions.cerne.filesystem]"} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q:\n%s", want, note)
		}
	}
}
