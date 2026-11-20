package hooks

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// F-0477 blocker: a script the shell is reading while the renewal rewrites it
// must keep its old bytes (old inode); the path gets the new ones.
func TestManagedWritesKeepTheOpenFileIntact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("old content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	open, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer open.Close()
	if st, err := writeIfChanged(path, []byte("new, longer content\n")); err != nil || st != StatusUpdated {
		t.Fatalf("writeIfChanged = %v, %v", st, err)
	}
	if got, _ := io.ReadAll(open); string(got) != "old content\n" {
		t.Fatalf("the open file now reads %q: it was rewritten in place", got)
	}
	if got, _ := os.ReadFile(path); string(got) != "new, longer content\n" {
		t.Fatalf("the path reads %q", got)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v", fi.Mode())
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".cerne-*")); len(left) > 0 {
		t.Errorf("temp files left: %v", left)
	}
}

// Every whole-file write of the install goes through the same path: the
// settings merge and a provider's configuration too.
func TestConfigMergesKeepTheOpenFileIntact(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"x": 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	open, _ := os.Open(settings)
	defer open.Close()
	if st, err := mergeSettingsFile(settings); err != nil || st != StatusUpdated {
		t.Fatalf("merge = %v, %v", st, err)
	}
	if got, _ := io.ReadAll(open); string(got) != `{"x": 1}` {
		t.Fatalf("settings.json was rewritten in place: %q", got)
	}
}
