package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// F-0354 crit. 1, 2 and 4: an explicit directory names the project; it is
// never a place to start walking up from.
func TestRequireConfigAt(t *testing.T) {
	root := t.TempDir()
	if err := SaveConfig(root, Config{ProjectID: "p", Backend: "/tmp/b"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CERNE_ROOT", root) // must not rescue an explicit directory

	// crit. 4: a directory holding .cernerc is used as it is.
	if err := RequireConfigAt(root); err != nil {
		t.Fatalf("RequireConfigAt(root): %v", err)
	}

	// crit. 1 + 2: no .cernerc in sub, and the project above is named.
	err := RequireConfigAt(sub)
	if !errors.Is(err, ErrNoProject) {
		t.Fatalf("want ErrNoProject, got %v", err)
	}
	want := "no .cernerc in " + sub + " (--dir names the project directory; it does not search parent directories)\n" +
		"nearest project above: " + root + " — pass --dir " + root + " to use it"
	if err.Error() != want {
		t.Fatalf("message = %q\nwant %q", err.Error(), want)
	}

	// crit. 1 without a project above: a single line, no hint.
	t.Setenv("CERNE_ROOT", "")
	outside := t.TempDir()
	err = RequireConfigAt(outside)
	want = "no .cernerc in " + outside + " (--dir names the project directory; it does not search parent directories)"
	if !errors.Is(err, ErrNoProject) || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}
