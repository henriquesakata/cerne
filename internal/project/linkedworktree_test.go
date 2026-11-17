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

// B-0016 critic 1: inside a git hook GIT_DIR points at the outer repository
// and overrides -C; a clone must still be refused.
func TestFindConfig_GitDirEnvDoesNotBypassGuard(t *testing.T) {
	isolateLookup(t)
	repo, _ := repoWithLateCerncerc(t)
	clone := filepath.Join(repo, "examples", "clone")
	mkdirs(t, clone)
	git(t, clone, "init", "-q")
	t.Setenv("GIT_DIR", filepath.Join(repo, ".git"))
	t.Setenv("GIT_WORK_TREE", repo)
	if _, err := findConfigDir(clone); !errors.Is(err, ErrNoProject) {
		t.Fatalf("want ErrNoProject under GIT_DIR, got %v", err)
	}
}

// B-0016 critic 2: CERNE_ROOT is an explicit request and wins over the
// worktree resolution.
func TestFindConfig_CerneRootBeatsLinkedWorktree(t *testing.T) {
	isolateLookup(t)
	repo, old := repoWithLateCerncerc(t)
	wt := filepath.Join(repo, ".claude", "worktrees", "wt1")
	git(t, repo, "worktree", "add", "-q", "-b", "wt1", wt, old)
	other := filepath.Join(t.TempDir(), "other")
	mkProject(t, other, "o")
	t.Setenv("CERNE_ROOT", other)
	got, err := findConfigDir(wt)
	if err != nil || got != other {
		t.Fatalf("got (%q, %v), want %q", got, err, other)
	}
	// A CERNE_ROOT without .cernerc does not hide the worktree's project.
	t.Setenv("CERNE_ROOT", t.TempDir())
	if got, err := findConfigDir(wt); err != nil || got != repo {
		t.Fatalf("invalid CERNE_ROOT: got (%q, %v), want %q", got, err, repo)
	}
}

// B-0016 critic 3: a project in a subdirectory of the repository (monorepo)
// is the same repository for a worktree made inside that subdirectory.
func TestFindConfig_MonorepoSubdirProject(t *testing.T) {
	isolateLookup(t)
	repo := t.TempDir()
	if r, err := filepath.EvalSymlinks(repo); err == nil {
		repo = r
	}
	git(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "a")
	git(t, repo, "commit", "-q", "-m", "old")
	proj := filepath.Join(repo, "sub")
	mkProject(t, proj, "p")
	git(t, repo, "add", "sub/.cernerc")
	git(t, repo, "commit", "-q", "-m", "init")
	wt := filepath.Join(proj, "wt")
	git(t, repo, "worktree", "add", "-q", "-b", "wt", wt, "HEAD~1")
	if got, err := findConfigDir(wt); err != nil || got != proj {
		t.Fatalf("got (%q, %v), want %q", got, err, proj)
	}
}

// B-0016 critic 3: a bare main repository with a linked worktree has no
// .cernerc anywhere: plain refusal.
func TestFindConfig_BareRepoWorktreeRefused(t *testing.T) {
	isolateLookup(t)
	src, _ := repoWithLateCerncerc(t)
	bare := filepath.Join(t.TempDir(), "bare.git")
	git(t, src, "clone", "-q", "--bare", src, bare)
	wt := filepath.Join(t.TempDir(), "wt")
	git(t, bare, "worktree", "add", "-q", wt, "HEAD~1")
	if _, err := findConfigDir(wt); !errors.Is(err, ErrNoProject) {
		t.Fatalf("want ErrNoProject, got %v", err)
	}
}
