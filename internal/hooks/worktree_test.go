package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstall_GitWorktree (B-0013): inside a linked worktree .git is a file,
// yet both commit hooks are written where git looks for them (the common
// .git/hooks), and the drift inspector reads them from there.
func TestInstall_GitWorktree(t *testing.T) {
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir,
			"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "wt")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(repo, "init")
	git(repo, "commit", "--allow-empty", "-m", "init")
	git(repo, "worktree", "add", wt, "-b", "t")

	hooksDir := git(wt, "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(wt, hooksDir)
	}
	if _, err := Install(wt); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"prepare-commit-msg", "commit-msg"} {
		if _, err := os.Stat(filepath.Join(hooksDir, name)); err != nil {
			t.Errorf("%s not written in %s: %v", name, hooksDir, err)
		}
	}
	for _, s := range GitHookStates(wt) {
		if s.State != FileOK {
			t.Errorf("inspect %s = %s, want ok", s.Name, s.State)
		}
	}
}
