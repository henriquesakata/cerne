package cli

import (
	"bytes"
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

// F-0354 crit. 5: every command declaring --dir (the top-level init excepted,
// by identity: it creates the .cernerc) refuses a folder without .cernerc.
// cobra runs only the nearest persistent pre-run, so the guard checks both
// halves: that hook is the root's (no command in between overrides it) and the
// root's hook refuses. DisableFlagParsing would hide the flag from the gate.
func TestEveryDirFlagCommandRefusesProjectless(t *testing.T) {
	projDir, _, _ := setupProject(t)
	sub := filepath.Join(projDir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	root := Root()
	initCmd, _, err := root.Find([]string{"init"})
	if err != nil || initCmd.Parent() != root {
		t.Fatalf("top-level init not found: %v", err)
	}
	checked := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, ch := range c.Commands() {
			walk(ch)
		}
		f := c.Flag("dir")
		if c == root || f == nil {
			return
		}
		if c.DisableFlagParsing {
			t.Errorf("%s declares --dir with DisableFlagParsing: cobra never marks it Changed, so the gate cannot see it", c.CommandPath())
		}
		for p := c; p != root; p = p.Parent() {
			if p.PersistentPreRun != nil || p.PersistentPreRunE != nil {
				t.Errorf("%s overrides the root pre-run, so --dir is not checked", p.CommandPath())
			}
		}
		if err := f.Value.Set(sub); err != nil {
			t.Errorf("%s: %v", c.CommandPath(), err)
			return
		}
		f.Changed = true
		err := root.PersistentPreRunE(c, nil)
		if c == initCmd {
			if err != nil {
				t.Errorf("init must be exempt, got %v", err)
			}
			return
		}
		checked++
		if !errors.Is(err, project.ErrNoProject) {
			t.Errorf("%s --dir <no .cernerc>: want ErrNoProject, got %v", c.CommandPath(), err)
		}
	}
	walk(root)
	if checked < 20 {
		t.Fatalf("guard walked only %d commands with --dir; Root() traversal is broken", checked)
	}
}

// F-0354: the init exemption is the top-level command, not any command named
// "init".
func TestGateExemptsOnlyTopLevelInit(t *testing.T) {
	root := &cobra.Command{Use: "cerne"}
	topInit := &cobra.Command{Use: "init"}
	group := &cobra.Command{Use: "group"}
	nested := &cobra.Command{Use: "init"}
	for _, c := range []*cobra.Command{topInit, nested} {
		c.Flags().String("dir", "", "")
	}
	root.AddCommand(topInit, group)
	group.AddCommand(nested)
	sub := t.TempDir()
	for _, c := range []*cobra.Command{topInit, nested} {
		f := c.Flag("dir")
		_ = f.Value.Set(sub)
		f.Changed = true
	}
	if err := refuseProjectlessDir(topInit); err != nil {
		t.Fatalf("top-level init must be exempt, got %v", err)
	}
	if err := refuseProjectlessDir(nested); !errors.Is(err, project.ErrNoProject) {
		t.Fatalf("nested init must not be exempt, got %v", err)
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

// F-0354 x F-0876: the root gate's refusal ends push/sync in the same single
// FALHA line as every other failure.
func TestGateRefusalEndsInFinalLine(t *testing.T) {
	projDir, _, _ := setupProject(t)
	sub := filepath.Join(projDir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"push", "sync"} {
		_, stderr, err := execCerneSplit(t, name, "--dir", sub)
		if !errors.Is(err, project.ErrNoProject) {
			t.Fatalf("%s: want ErrNoProject, got %v", name, err)
		}
		if n := strings.Count(stderr, "cerne: resultado=FALHA "); n != 1 || !strings.HasPrefix(lastLine(stderr), "cerne: resultado=FALHA no .cernerc in "+sub) {
			t.Fatalf("%s: want one FALHA line, last, got:\n%s", name, stderr)
		}
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

// gitRun runs git in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// nestedClone creates a git repository (no .cernerc) inside projDir and
// returns it, as a fixture or example clone would sit in a cerne project.
func nestedClone(t *testing.T, projDir string) string {
	t.Helper()
	clone := filepath.Join(projDir, "examples", "clone")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, clone, "init", "-q")
	return clone
}

// F-0372 crit. 1 + 2: without --dir, a command run inside a nested git
// repository refuses before touching the backend and names the git root, the
// project above and the --dir escape — which works.
func TestNestedGitRefusesWithoutDir(t *testing.T) {
	t.Setenv("CERNE_ROOT", "")
	projDir, origin, _ := setupProject(t)
	clone := nestedClone(t, projDir)
	t.Chdir(clone)
	before := originHead(t, origin)

	want := "no cerne project in this git repository (" + clone + "); the nearest .cernerc is at " + projDir +
		", outside it — use --dir \"" + projDir + "\" (or set CERNE_ROOT=\"" + projDir + "\") to work on that project on purpose"
	for _, args := range [][]string{
		{"feature", "new", "Boletos", "--context", "x"},
		{"status"},
	} {
		out, err := execCerne(t, args...)
		if !errors.Is(err, project.ErrNoProject) {
			t.Fatalf("%v: want ErrNoProject, got err=%v out=%s", args, err, out)
		}
		if err.Error() != want {
			t.Fatalf("%v: message = %q\nwant %q", args, err.Error(), want)
		}
	}
	if after := originHead(t, origin); after != before {
		t.Fatalf("backend gained a commit: %s -> %s", before, after)
	}
	if out, err := execCerne(t, "status", "--dir", projDir); err != nil {
		t.Fatalf("the --dir escape: %v\n%s", err, out)
	}
}

// F-0372 crit. 3 + 5: a subdirectory of the project's own repository, and a
// git worktree of it carrying the versioned .cernerc (this repository's
// .claude/worktrees/<name>/ layout), resolve as before.
func TestNestedGitSameRepoAndWorktreeResolve(t *testing.T) {
	t.Setenv("CERNE_ROOT", "")
	projDir, _, _ := setupProject(t)
	gitRun(t, projDir, "init", "-q")
	gitRun(t, projDir, "add", ".cernerc")
	gitRun(t, projDir, "commit", "-q", "-m", "init")
	sub := filepath.Join(projDir, "internal", "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if out, err := execCerne(t, "status"); err != nil {
		t.Fatalf("status from a subdir of the project's repo: %v\n%s", err, out)
	}

	wt := filepath.Join(projDir, ".claude", "worktrees", "wt")
	gitRun(t, projDir, "worktree", "add", "-q", "-b", "wt", wt)
	if fi, err := os.Lstat(filepath.Join(wt, ".git")); err != nil || fi.IsDir() {
		t.Fatalf("worktree .git must be a file: %v", err)
	}
	t.Chdir(wt)
	if out, err := execCerne(t, "status"); err != nil {
		t.Fatalf("status from the project's git worktree: %v\n%s", err, out)
	}
}

// F-0372 crit. 6: init inside a nested git repository is exempt and creates
// the nested project.
func TestNestedGitInitWorks(t *testing.T) {
	t.Setenv("CERNE_ROOT", "")
	projDir, _, _ := setupProject(t)
	clone := nestedClone(t, projDir)
	t.Chdir(clone)
	if out, err := execInit(t, "--name", "inner", "--backend", newOrigin(t)); err != nil {
		t.Fatalf("init in a nested clone: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "status"); err != nil {
		t.Fatalf("status in the new nested project: %v\n%s", err, out)
	}
}

// F-0372 crit. 7: CERNE_ROOT still wins at the git boundary, and is announced.
func TestNestedGitCerneRootIsAnnounced(t *testing.T) {
	projDir, _, _ := setupProject(t)
	clone := nestedClone(t, projDir)
	t.Setenv("CERNE_ROOT", projDir)
	t.Chdir(clone)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	out, cerr := execCerne(t, "status")
	os.Stderr = old
	w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if cerr != nil {
		t.Fatalf("status via CERNE_ROOT: %v\n%s", cerr, out)
	}
	if want := "using CERNE_ROOT=" + projDir; !strings.Contains(buf.String(), want) {
		t.Fatalf("adoption not announced (want %q):\n%s", want, buf.String())
	}
}

// F-0372 / N-0021: a hook fired inside a nested git repository fails open —
// no error, no injected rules from the project above.
func TestNestedGitHookFailsOpen(t *testing.T) {
	t.Setenv("CERNE_ROOT", "")
	for _, env := range []string{"GEMINI_PROJECT_DIR", "GEMINI_CWD", "CURSOR_PROJECT_DIR", "CLAUDE_PROJECT_DIR"} {
		t.Setenv(env, "")
	}
	projDir, _, _ := setupProject(t)
	clone := nestedClone(t, projDir)
	t.Chdir(clone)
	out, err := execCerneStdin(t, hookJSON(t, "s", clone, "", nil, nil), "hook", "session-context")
	if err != nil || strings.TrimSpace(out) != "" {
		t.Fatalf("hook in a nested clone: err=%v out=%s", err, out)
	}
	// Positive control: the same hook from the project dir does inject.
	t.Chdir(projDir)
	out, err = execCerneStdin(t, hookJSON(t, "s", projDir, "", nil, nil), "hook", "session-context")
	if err != nil || !strings.Contains(out, "ACTIVE WORK RIGHT NOW") {
		t.Fatalf("hook in the project: err=%v out=%s", err, out)
	}
}

// F-0876 / F-0372: with CERNE_ROOT set, noteCerneRoot stays silent when the
// walk wins (the cwd belongs to another project).
func TestNestedGitCerneRootSilentWhenWalkWins(t *testing.T) {
	projDir, _, _ := setupProject(t)
	other := t.TempDir()
	if err := project.SaveConfig(other, project.Config{ProjectID: "other", Backend: "/tmp/b"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CERNE_ROOT", other)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	noteCerneRoot(projDir, projDir)
	os.Stderr = old
	w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if buf.Len() != 0 {
		t.Fatalf("no note expected when the walk wins:\n%s", buf.String())
	}
}

// F-0372 round 1: the F-0354 hint for an explicit --dir comes from the same
// walk as resolution; across a git boundary it still names the project above
// (the user named --dir, so the hint is the escape) and resolves nothing.
func TestNestedGitExplicitDirHintNamesProjectAbove(t *testing.T) {
	t.Setenv("CERNE_ROOT", "")
	projDir, origin, _ := setupProject(t)
	clone := nestedClone(t, projDir)
	before := originHead(t, origin)
	_, err := execCerne(t, "feature", "new", "Boletos", "--context", "x", "--dir", clone)
	want := "no .cernerc in " + clone + " (--dir names the project directory; it does not search parent directories)\n" +
		"nearest project above: " + projDir + " — pass --dir " + projDir + " to use it"
	if !errors.Is(err, project.ErrNoProject) || err.Error() != want {
		t.Fatalf("got %v\nwant %s", err, want)
	}
	if after := originHead(t, origin); after != before {
		t.Fatalf("backend gained a commit: %s -> %s", before, after)
	}
}

// F-0372 round 1: the ui resolves its first project from the cwd through
// resolveProject; from a nested clone it refuses and the backend is untouched.
func TestNestedGitUIResolverRefuses(t *testing.T) {
	t.Setenv("CERNE_ROOT", "")
	projDir, origin, _ := setupProject(t)
	clone := nestedClone(t, projDir)
	before := originHead(t, origin)
	if _, _, _, _, err := uiResolver(clone); !errors.Is(err, project.ErrNoProject) {
		t.Fatalf("uiResolver from a nested clone: want ErrNoProject, got %v", err)
	}
	if after := originHead(t, origin); after != before {
		t.Fatalf("backend gained a commit: %s -> %s", before, after)
	}
}

// F-0372 round 1: hook install never resolves a project — it writes into the
// directory it runs in. From a nested clone that is the clone, never the
// project above, and the backend is untouched.
func TestNestedGitHookInstallStaysLocal(t *testing.T) {
	t.Setenv("CERNE_ROOT", "")
	projDir, origin, _ := setupProject(t)
	clone := nestedClone(t, projDir)
	t.Chdir(clone)
	before := originHead(t, origin)
	aboveSettings := filepath.Join(projDir, ".claude", "settings.json")
	aboveBefore, _ := os.ReadFile(aboveSettings) // init may have written it
	if out, err := execCerne(t, "hook", "install", "--provider", "claude"); err != nil {
		t.Fatalf("hook install: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(clone, ".claude", "settings.json")); err != nil {
		t.Fatalf("settings not written in the clone: %v", err)
	}
	if aboveAfter, _ := os.ReadFile(aboveSettings); !bytes.Equal(aboveAfter, aboveBefore) {
		t.Fatalf("project above's settings changed")
	}
	if after := originHead(t, origin); after != before {
		t.Fatalf("backend gained a commit: %s -> %s", before, after)
	}
}
