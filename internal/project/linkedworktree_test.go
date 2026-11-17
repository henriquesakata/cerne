package project

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "protocol.file.allow=always"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// repoWithLateCerncerc builds a repo with a commit BEFORE .cernerc and one
// after, returning the repo dir and the old commit.
func repoWithLateCerncerc(t *testing.T) (repo, oldCommit string) {
	t.Helper()
	repo = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	git(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "a.txt")
	git(t, repo, "commit", "-q", "-m", "before cerne")
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	oldCommit = string(out[:len(out)-1])
	mkProject(t, repo, "p")
	git(t, repo, "add", ".cernerc")
	git(t, repo, "commit", "-q", "-m", "cerne init")
	return repo, oldCommit
}

// B-0016: a linked worktree of the project's own repository, based before
// .cernerc existed, resolves to the main checkout.
func TestFindConfig_LinkedWorktreeBeforeInit(t *testing.T) {
	isolateLookup(t)
	repo, old := repoWithLateCerncerc(t)
	wt := filepath.Join(repo, ".claude", "worktrees", "wt1")
	git(t, repo, "worktree", "add", "-q", "-b", "wt1", wt, old)
	sub := filepath.Join(wt, "pkg")
	mkdirs(t, sub)

	for _, from := range []string{wt, sub} {
		got, err := findConfigDir(from)
		if err != nil || got != repo {
			t.Fatalf("from %s: got (%q, %v), want %q", from, got, err, repo)
		}
		if p := LinkedWorktreeProject(from); p != repo {
			t.Fatalf("LinkedWorktreeProject(%s) = %q, want %q", from, p, repo)
		}
		if !FoundByWalk(from) {
			t.Fatalf("FoundByWalk(%s) = false", from)
		}
	}
	if p := LinkedWorktreeProject(repo); p != "" {
		t.Fatalf("main checkout reported as linked worktree: %q", p)
	}
	if _, err := os.Stat(filepath.Join(wt, ".cernerc")); err == nil {
		t.Fatal("a .cernerc appeared in the worktree")
	}
}

// B-0016: a separate clone and a submodule of the project stay refused.
func TestFindConfig_CloneAndSubmoduleStillRefused(t *testing.T) {
	isolateLookup(t)
	repo, _ := repoWithLateCerncerc(t)

	clone := filepath.Join(repo, "examples", "clone")
	mkdirs(t, clone)
	git(t, clone, "init", "-q")
	if _, err := findConfigDir(clone); !errors.Is(err, ErrNoProject) {
		t.Fatalf("clone: want ErrNoProject, got %v", err)
	}

	other := t.TempDir()
	git(t, other, "init", "-q")
	if err := os.WriteFile(filepath.Join(other, "b"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, other, "add", "b")
	git(t, other, "commit", "-q", "-m", "x")
	git(t, repo, "submodule", "add", "-q", other, "vendor/sub")
	sub := filepath.Join(repo, "vendor", "sub")
	if _, err := findConfigDir(sub); !errors.Is(err, ErrNoProject) {
		t.Fatalf("submodule: want ErrNoProject, got %v", err)
	}
	if p := LinkedWorktreeProject(sub); p != "" {
		t.Fatalf("submodule reported as linked worktree: %q", p)
	}
}
