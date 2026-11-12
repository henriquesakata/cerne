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
// explicit --dir . from a subfolder does not climb either.
func TestExplicitDirDotDoesNotClimb(t *testing.T) {
	projDir, _, _ := setupProject(t)
	sub := filepath.Join(projDir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if _, err := execCerne(t, "status", "--dir", "."); !errors.Is(err, project.ErrNoProject) {
		t.Fatalf("--dir . from a subfolder: want ErrNoProject, got %v", err)
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
