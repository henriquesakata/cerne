package gitback

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// initRepoWith creates a standalone git repo (no remote) at a temp dir with the
// given files committed on the current branch. Returns the repo path.
func initRepoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	for name, content := range files {
		writeFile(t, dir, name, content)
		if _, err := wt.Add(name); err != nil {
			t.Fatalf("Add %s: %v", name, err)
		}
	}
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("Commit init: %v", err)
	}
	return dir
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	sha, err := HeadSHA(dir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	return sha
}

func TestCommitPathCommitsSingleFile(t *testing.T) {
	dir := initRepoWith(t, map[string]string{"docs.lock": "v1"})
	before := headOf(t, dir)
	writeFile(t, dir, "docs.lock", "v2")

	sha, err := CommitPath(dir, "docs.lock", "chore(docs): pin docs.lock")
	if err != nil {
		t.Fatalf("CommitPath: %v", err)
	}
	if !shaHex.MatchString(sha) {
		t.Fatalf("sha = %q, want 40-char hex", sha)
	}
	if sha == before {
		t.Fatalf("HEAD did not advance")
	}
	// Worktree clean for docs.lock now.
	repo, _ := git.PlainOpen(dir)
	wt, _ := repo.Worktree()
	st, _ := wt.Status()
	if !st.IsClean() {
		t.Fatalf("worktree not clean after pin: %v", st)
	}
	// Commit message and single-file change.
	headRef, _ := repo.Head()
	c, err := repo.CommitObject(headRef.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	if c.Message != "chore(docs): pin docs.lock" {
		t.Fatalf("message = %q", c.Message)
	}
}

func TestCommitPathNothingToCommit(t *testing.T) {
	dir := initRepoWith(t, map[string]string{"docs.lock": "v1"})
	_, err := CommitPath(dir, "docs.lock", "msg")
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("err = %v, want ErrNothingToCommit", err)
	}
}

func TestCommitPathLeavesUnstagedUntouched(t *testing.T) {
	dir := initRepoWith(t, map[string]string{"docs.lock": "v1", "other.txt": "a"})
	writeFile(t, dir, "docs.lock", "v2")
	writeFile(t, dir, "other.txt", "b") // unstaged change to an unrelated file

	if _, err := CommitPath(dir, "docs.lock", "pin"); err != nil {
		t.Fatalf("CommitPath: %v", err)
	}
	repo, _ := git.PlainOpen(dir)
	wt, _ := repo.Worktree()
	st, _ := wt.Status()
	// other.txt still modified & unstaged (not swept into the pin commit).
	if st.File("other.txt").Worktree != git.Modified {
		t.Fatalf("other.txt worktree = %v, want Modified", st.File("other.txt").Worktree)
	}
	// The pin commit changed only docs.lock.
	headRef, _ := repo.Head()
	head, _ := repo.CommitObject(headRef.Hash())
	parent, _ := head.Parent(0)
	patch, _ := parent.Patch(head)
	if n := len(patch.FilePatches()); n != 1 {
		t.Fatalf("pin commit touched %d files, want 1 (docs.lock only)", n)
	}
}

func TestCommitPathNonRepoSkips(t *testing.T) {
	dir := t.TempDir() // not a git repo
	if err := os.WriteFile(filepath.Join(dir, "docs.lock"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := CommitPath(dir, "docs.lock", "msg")
	if !errors.Is(err, ErrNotRepo) {
		t.Fatalf("err = %v, want ErrNotRepo", err)
	}
}

func TestCommitPathNoPush(t *testing.T) {
	// Repo has NO remote; CommitPath must commit on the current HEAD without
	// attempting a push (a push would error on the missing remote).
	dir := initRepoWith(t, map[string]string{"docs.lock": "v1"})
	writeFile(t, dir, "docs.lock", "v2")
	sha, err := CommitPath(dir, "docs.lock", "pin")
	if err != nil {
		t.Fatalf("CommitPath on remote-less repo: %v", err)
	}
	if sha != headOf(t, dir) {
		t.Fatalf("returned sha %q != HEAD %q", sha, headOf(t, dir))
	}
}

func TestCommitPathOtherStagedSkips(t *testing.T) {
	dir := initRepoWith(t, map[string]string{"docs.lock": "v1", "other.txt": "a"})
	before := headOf(t, dir)
	writeFile(t, dir, "docs.lock", "v2")
	// Stage an unrelated change.
	writeFile(t, dir, "other.txt", "b")
	repo, _ := git.PlainOpen(dir)
	wt, _ := repo.Worktree()
	if _, err := wt.Add("other.txt"); err != nil {
		t.Fatal(err)
	}

	_, err := CommitPath(dir, "docs.lock", "pin")
	if !errors.Is(err, ErrOtherStaged) {
		t.Fatalf("err = %v, want ErrOtherStaged", err)
	}
	if headOf(t, dir) != before {
		t.Fatalf("HEAD advanced despite skip")
	}
	// User's staged change untouched.
	st, _ := wt.Status()
	if st.File("other.txt").Staging != git.Modified {
		t.Fatalf("other.txt staging = %v, want Modified (preserved)", st.File("other.txt").Staging)
	}
}

// TestCommitPathAvoidsWorktreeStatus is the F-0153 guard: CommitPath runs on
// the USER's project worktree (possibly a huge monorepo), where go-git's
// Worktree.Status() hashes the entire tree — 80s+ per call on the hub, the
// measured cause of 113s-average snapshot phases. The rewritten CommitPath
// lives in commitpath.go and must never call .Status() (nor plain wt.Add,
// which calls Status internally — AddWithOptions{SkipStatus} is the
// sanctioned path).
func TestCommitPathAvoidsWorktreeStatus(t *testing.T) {
	src, err := os.ReadFile("commitpath.go")
	if err != nil {
		t.Fatalf("commitpath.go must exist (CommitPath extracted from gitback.go): %v", err)
	}
	for _, banned := range []string{".Status()", "wt.Add("} {
		if bytes.Contains(src, []byte(banned)) {
			t.Errorf("commitpath.go contains %q — it would re-scan the user's whole worktree (F-0153)", banned)
		}
	}
}

// TestCommitPathStagedDeletionSkips pins one more ErrOtherStaged shape: a
// deletion staged in the index (file removed from index, present in HEAD)
// must also block the pin.
func TestCommitPathStagedDeletionSkips(t *testing.T) {
	dir := initRepoWith(t, map[string]string{"docs.lock": "v1", "other.txt": "a"})
	writeFile(t, dir, "docs.lock", "v2")
	repo, _ := git.PlainOpen(dir)
	wt, _ := repo.Worktree()
	if _, err := wt.Remove("other.txt"); err != nil {
		t.Fatal(err)
	}
	_, err := CommitPath(dir, "docs.lock", "pin")
	if !errors.Is(err, ErrOtherStaged) {
		t.Fatalf("err = %v, want ErrOtherStaged for a staged deletion", err)
	}
}
