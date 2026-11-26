package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Critic #2/#9: if ANY folder is machine-specific, nothing is written to the
// project file, and ONE snippet carries every folder.
func TestInstallCodexSandboxAnyMachineFolderWritesNothing(t *testing.T) {
	proj, home := codexProject(t, "x")
	backend := filepath.Join(home, "backends", "demo.git")
	bareRepo(t, backend) // F-0512 review #1b: only a repository is opened
	if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte("project_id = \"demo\"\nbackend = \""+backend+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cache)

	results, err := Install(proj, ProviderCodex)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(proj, ".codex", "config.toml"))
	if strings.Contains(string(raw), "permissions") {
		t.Fatalf("project file must carry no profile:\n%s", raw)
	}
	var notes []string
	for _, r := range results {
		if strings.HasPrefix(r.Asset, "codex: config.toml (sandbox") {
			notes = append(notes, r.Note)
		}
	}
	if len(notes) != 1 {
		t.Fatalf("want ONE sandbox result, got %d: %v", len(notes), notes)
	}
	for _, want := range []string{filepath.Join(cache, "demo"), "~/backends/demo.git", "--add-dir \"" + backend + "\""} {
		if !strings.Contains(notes[0], want) {
			t.Errorf("snippet missing %q:\n%s", want, notes[0])
		}
	}
}

// Critic #8: a folder named "..foo" inside the project is inside the project.
func TestInstallCodexSandboxDotDotPrefixedFolderIsInside(t *testing.T) {
	proj, _ := codexProject(t, "x")
	backend := filepath.Join(proj, "..foo", "b.git")
	if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte("project_id = \"demo\"\nbackend = \""+backend+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(proj, ProviderCodex); err != nil {
		t.Fatal(err)
	}
	c, raw := readCodexCfg(t, proj)
	if c.DefaultPermissions != "cerne" || strings.Contains(raw, "..foo") {
		t.Fatalf("backend inside the project must be skipped and the block written:\n%s", raw)
	}
}
