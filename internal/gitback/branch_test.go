package gitback

import (
	"errors"
	"os/exec"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

func TestCurrentBranchOnBranch(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	writeFile(t, dir, "a.txt", "hi")
	if _, err := CommitAll(dir, "init"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.Checkout(&git.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("feature/x"),
		Create: true,
	}); err != nil {
		t.Fatalf("checkout -b feature/x: %v", err)
	}
	got, err := CurrentBranch(dir)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if got != "feature/x" {
		t.Fatalf("CurrentBranch = %q, want %q", got, "feature/x")
	}
}

// An unborn branch (fresh init, no commits) still has a name: HEAD is symbolic.
func TestCurrentBranchUnborn(t *testing.T) {
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatalf("init: %v", err)
	}
	got, err := CurrentBranch(dir)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if got != "master" {
		t.Fatalf("CurrentBranch = %q, want %q (go-git init default)", got, "master")
	}
}

// Criterion 6: detached HEAD reports "" (never equals a configured pin branch).
func TestCurrentBranchDetached(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	writeFile(t, dir, "a.txt", "hi")
	sha, err := CommitAll(dir, "init")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.Checkout(&git.CheckoutOptions{Hash: plumbing.NewHash(sha)}); err != nil {
		t.Fatalf("detach: %v", err)
	}
	got, err := CurrentBranch(dir)
	if err != nil {
		t.Fatalf("CurrentBranch detached: %v", err)
	}
	if got != "" {
		t.Fatalf("CurrentBranch detached = %q, want empty", got)
	}
}

func TestCurrentBranchNotRepo(t *testing.T) {
	_, err := CurrentBranch(t.TempDir())
	if !errors.Is(err, ErrNotRepo) {
		t.Fatalf("CurrentBranch err = %v, want ErrNotRepo", err)
	}
}

// Criterion 7: a parallel linked worktree reports its own branch, not the main
// checkout's. Needs the git binary (go-git cannot create worktrees); the repo
// it produces also carries extensions.worktreeConfig-style layout (gitdir file
// + commondir), so this doubles as the tolerant-open coverage for reads.
func TestCurrentBranchLinkedWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary unavailable")
	}
	main := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main", main},
		{"-C", main, "config", "user.email", "t@local"},
		{"-C", main, "config", "user.name", "t"},
		{"-C", main, "commit", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	wtDir := t.TempDir() + "/wt"
	if out, err := exec.Command("git", "-C", main, "worktree", "add", "-b", "wt-branch", wtDir).CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, out)
	}
	got, err := CurrentBranch(wtDir)
	if err != nil {
		t.Fatalf("CurrentBranch(worktree): %v", err)
	}
	if got != "wt-branch" {
		t.Fatalf("CurrentBranch(worktree) = %q, want %q", got, "wt-branch")
	}
	// The main checkout is unaffected.
	got, err = CurrentBranch(main)
	if err != nil {
		t.Fatalf("CurrentBranch(main): %v", err)
	}
	if got != "main" {
		t.Fatalf("CurrentBranch(main) = %q, want %q", got, "main")
	}
}
