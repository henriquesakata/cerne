package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/project"
)

func originHead(t *testing.T, origin string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", origin, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse origin: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// F-0354 crit. 1 + 2: --dir at a folder without .cernerc inside a project tree
// refuses, names the project above, and leaves the backend untouched.
func TestExplicitDirWithoutCernercRefuses(t *testing.T) {
	projDir, origin, _ := setupProject(t)
	sub := filepath.Join(projDir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	before := originHead(t, origin)

	out, err := execCerne(t, "feature", "new", "Boletos", "--context", "x", "--dir", sub)
	if err == nil || !errors.Is(err, project.ErrNoProject) {
		t.Fatalf("want ErrNoProject, got err=%v out=%s", err, out)
	}
	want := "no .cernerc in " + sub + " (--dir names the project directory; it does not search parent directories)\n" +
		"nearest project above: " + projDir + " — pass --dir " + projDir + " to use it"
	if err.Error() != want {
		t.Fatalf("message = %q\nwant %q", err.Error(), want)
	}
	if after := originHead(t, origin); after != before {
		t.Fatalf("backend gained a commit: %s -> %s", before, after)
	}
}

// F-0354: the decision is by origin of the directory, not by value: an
// explicit --dir . (or a relative path) does not climb either, and the message
// names the ABSOLUTE path.
func TestExplicitRelativeDirDoesNotClimb(t *testing.T) {
	projDir, _, _ := setupProject(t)
	sub := filepath.Join(projDir, "sub")
	if err := os.MkdirAll(filepath.Join(sub, "evidence", "sim"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ flag, abs string }{
		{".", wd},
		{filepath.Join("evidence", "sim"), filepath.Join(wd, "evidence", "sim")},
	} {
		_, err := execCerne(t, "status", "--dir", tc.flag)
		if !errors.Is(err, project.ErrNoProject) {
			t.Fatalf("--dir %s: want ErrNoProject, got %v", tc.flag, err)
		}
		above, ok := "", false
		for d := filepath.Dir(tc.abs); ; d = filepath.Dir(d) {
			if _, e := os.Stat(filepath.Join(d, ".cernerc")); e == nil {
				above, ok = d, true
				break
			}
			if d == filepath.Dir(d) {
				break
			}
		}
		want := "no .cernerc in " + tc.abs + " (--dir names the project directory; it does not search parent directories)"
		if ok {
			want += "\nnearest project above: " + above + " — pass --dir " + above + " to use it"
		}
		if err.Error() != want {
			t.Fatalf("--dir %s: message = %q\nwant %q", tc.flag, err.Error(), want)
		}
	}
}

// F-0354 crit. 3: without --dir the search still climbs from the cwd.
func TestNoDirClimbsFromCwd(t *testing.T) {
	projDir, _, _ := setupProject(t)
	sub := filepath.Join(projDir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if out, err := execCerne(t, "status"); err != nil {
		t.Fatalf("status from a subfolder: %v\n%s", err, out)
	}
}

// F-0354 crit. 4: --dir at a folder holding .cernerc works as before.
func TestExplicitDirWithCernercWorks(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if out, err := execCerne(t, "status", "--dir", projDir); err != nil {
		t.Fatalf("status --dir: %v\n%s", err, out)
	}
}

// F-0354 crit. 5: every command declaring --dir (init excepted: it creates the
// .cernerc) refuses a folder without .cernerc. cobra runs only the nearest
// persistent pre-run, so the guard checks both halves: that hook is the root's
// (no command in between overrides it) and the root's hook refuses.
func TestEveryDirFlagCommandRefusesProjectless(t *testing.T) {
	projDir, _, _ := setupProject(t)
	sub := filepath.Join(projDir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	root := Root()
	checked := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, ch := range c.Commands() {
			walk(ch)
		}
		if c == root || c.Name() == "init" || c.Flag("dir") == nil {
			return
		}
		checked++
		for p := c; p != root; p = p.Parent() {
			if p.PersistentPreRun != nil || p.PersistentPreRunE != nil {
				t.Errorf("%s overrides the root pre-run, so --dir is not checked", p.CommandPath())
			}
		}
		f := c.Flag("dir")
		if err := f.Value.Set(sub); err != nil {
			t.Errorf("%s: %v", c.CommandPath(), err)
			return
		}
		f.Changed = true
		if err := root.PersistentPreRunE(c, nil); !errors.Is(err, project.ErrNoProject) {
			t.Errorf("%s --dir <no .cernerc>: want ErrNoProject, got %v", c.CommandPath(), err)
		}
	}
	walk(root)
	if checked < 20 {
		t.Fatalf("guard walked only %d commands with --dir; Root() traversal is broken", checked)
	}
}

// F-0354 crit. 3: resolvePaths is not the rule (the root gate is): an internal
// caller handing it a computed subfolder path still climbs to the project.
func TestResolvePathsClimbsForComputedPath(t *testing.T) {
	projDir, _, _ := setupProject(t)
	sub := filepath.Join(projDir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, err := resolvePaths(sub)
	if err != nil || ctx.projDir != projDir {
		t.Fatalf("resolvePaths(sub) = %q, %v; want %q", ctx.projDir, err, projDir)
	}
}

// F-0354: the gate only checks that .cernerc exists; a malformed one is not
// the F-0354 refusal for commands that never read it.
func TestGateDoesNotParseCerncerc(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".cernerc"), []byte("not [valid toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execCerne(t, "hook", "install", "--dir", dir)
	if err != nil {
		t.Fatalf("hook install with a malformed .cernerc: %v\n%s", err, out)
	}
}
