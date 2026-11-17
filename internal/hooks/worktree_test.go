package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir,
		"-c", "user.email=t@t", "-c", "user.name=t", "-c", "protocol.file.allow=always"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "init")
}

func wantHooks(t *testing.T, proj string) {
	t.Helper()
	hooksDir := gitIn(t, proj, "rev-parse", "--git-path", "hooks")
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(proj, hooksDir)
	}
	if _, err := Install(proj); err != nil {
		t.Fatal(err)
	}
	for name, marker := range map[string]string{"prepare-commit-msg": "commit-trailer", "commit-msg": guardMarker} {
		p := filepath.Join(hooksDir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s not written in %s: %v", name, hooksDir, err)
			continue
		}
		if !strings.Contains(string(b), marker) {
			t.Errorf("%s lacks marker %q", name, marker)
		}
		if fi, _ := os.Stat(p); runtime.GOOS != "windows" && fi.Mode()&0o111 == 0 {
			t.Errorf("%s not executable", name)
		}
	}
	for _, s := range GitHookStates(proj) {
		if s.State != FileOK {
			t.Errorf("inspect %s = %s, want ok", s.Name, s.State)
		}
	}
}

// TestInstall_GitSubmodule (B-0013): a submodule's .git is a file too.
func TestInstall_GitSubmodule(t *testing.T) {
	root := t.TempDir()
	sub, super := filepath.Join(root, "sub"), filepath.Join(root, "super")
	newRepo(t, sub)
	newRepo(t, super)
	gitIn(t, super, "submodule", "add", sub, "mod")
	wantHooks(t, filepath.Join(super, "mod"))
}

// TestInstall_WorktreeCustomHooksPath (B-0013): a repo-level core.hooksPath
// is honored from a worktree: both hooks skipped, nothing written there.
func TestInstall_WorktreeCustomHooksPath(t *testing.T) {
	root := t.TempDir()
	repo, wt := filepath.Join(root, "repo"), filepath.Join(root, "wt")
	newRepo(t, repo)
	gitIn(t, repo, "config", "core.hooksPath", ".husky/_")
	gitIn(t, repo, "worktree", "add", wt, "-b", "t")
	results, err := Install(wt)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range results {
		if r.Asset == "prepare-commit-msg" || r.Asset == "commit-msg" {
			n++
			if r.Status != StatusSkipped || !strings.Contains(r.Note, "custom core.hooksPath") {
				t.Errorf("%s = %+v, want skipped custom core.hooksPath", r.Asset, r)
			}
		}
	}
	if n != 2 {
		t.Fatalf("saw %d commit hook results, want 2", n)
	}
	for _, d := range []string{wt, repo} {
		if _, err := os.Stat(filepath.Join(d, ".husky", "_", "commit-msg")); err == nil {
			t.Errorf("hook written under %s/.husky/_", d)
		}
	}
}

// TestInstall_GitWorktree (B-0013): inside a linked worktree .git is a file,
// yet both commit hooks are written where git looks for them (the common
// .git/hooks), and the drift inspector reads them from there.
func TestInstall_GitWorktree(t *testing.T) {
	root := t.TempDir()
	repo, wt := filepath.Join(root, "repo"), filepath.Join(root, "wt")
	newRepo(t, repo)
	gitIn(t, repo, "worktree", "add", wt, "-b", "t")
	wantHooks(t, wt)
}
