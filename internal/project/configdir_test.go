package project

import (
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/rockt/cerne/internal/models"
)

// TestConfigDirIsGlobal: criterion 4 — every project resolves the same
// user-level dir, and the catalog in it is re-read on every Load (no
// per-project or per-process copy), so an update made by one project/process
// reaches the next read of any other.
func TestConfigDirIsGlobal(t *testing.T) {
	global := t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", global)

	var dirs []string
	for _, proj := range []string{t.TempDir(), t.TempDir()} {
		t.Chdir(proj)
		d, err := ConfigDir()
		if err != nil {
			t.Fatalf("ConfigDir: %v", err)
		}
		dirs = append(dirs, d)
	}
	if dirs[0] != global || dirs[1] != global {
		t.Fatalf("config dirs = %v, want both %s", dirs, global)
	}

	first, _ := models.Load(dirs[0])
	if _, ok := first.Find("vendor-x-1"); ok {
		t.Fatal("entry present before the write")
	}
	body := `{"entries":[{"id":"vendor-x-1","label":"Vendor X 1","provider":"vendor"}]}`
	if err := os.WriteFile(filepath.Join(global, models.FileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, d := range dirs {
		cat, warn := models.Load(d)
		if _, ok := cat.Find("vendor-x-1"); !ok || warn != nil {
			t.Fatalf("project %d did not see the global update (warn %v)", i, warn)
		}
	}
}

// TestConfigDirDefault: without the override the dir is <UserConfigDir>/cerne.
func TestConfigDirDefault(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", "")
	root, err := os.UserConfigDir()
	if err != nil {
		t.Skip("no user config dir on this machine")
	}
	got, err := ConfigDir()
	if err != nil || got != filepath.Join(root, "cerne") {
		t.Fatalf("ConfigDir = %q, %v; want %s", got, err, filepath.Join(root, "cerne"))
	}
}
