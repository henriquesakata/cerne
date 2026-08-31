package gitback

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The native-git fast path is only safe if it is indistinguishable from the
// go-git one. These tests run the same scenario through both backends and
// compare the observable result — swapping gitBin is the switch.

func withoutGitBin(t *testing.T) {
	t.Helper()
	prev := gitBin
	gitBin = func() string { return "" }
	t.Cleanup(func() { gitBin = prev })
}

func requireGitBin(t *testing.T) {
	t.Helper()
	if gitBin() == "" {
		t.Skip("git binary unavailable")
	}
}

// dirtyRepo seeds a repo and dirties it the three ways the reconcile loop has
// to capture: a modification, an addition (untracked) and a deletion.
func dirtyRepo(t *testing.T) string {
	t.Helper()
	dir := initRepoWith(t, map[string]string{"keep.md": "keep\n", "edit.md": "before\n", "gone.md": "gone\n"})
	writeFile(t, dir, "edit.md", "after\n")
	writeFile(t, dir, "added.md", "new\n")
	if err := os.Remove(filepath.Join(dir, "gone.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	return dir
}

func TestWorktreeChangesBackendsAgree(t *testing.T) {
	requireGitBin(t)
	exec, err := WorktreeChanges(dirtyRepo(t))
	if err != nil {
		t.Fatalf("exec WorktreeChanges: %v", err)
	}
	dir := dirtyRepo(t)
	withoutGitBin(t)
	gogit, err := WorktreeChanges(dir)
	if err != nil {
		t.Fatalf("go-git WorktreeChanges: %v", err)
	}
	want := []Change{
		{Path: "added.md"}, {Path: "edit.md"}, {Path: "gone.md", Deleted: true},
	}
	if !reflect.DeepEqual(gogit, want) {
		t.Fatalf("go-git changes = %+v, want %+v", gogit, want)
	}
	if !reflect.DeepEqual(exec, gogit) {
		t.Fatalf("exec changes = %+v, want %+v", exec, gogit)
	}
}

func TestCommitPathsBackendsAgree(t *testing.T) {
	requireGitBin(t)
	paths := []string{"edit.md", "added.md", "gone.md"}

	commitAndRead := func(dir string) (string, []Change) {
		t.Helper()
		if _, err := CommitPaths(dir, paths, "probe"); err != nil {
			t.Fatalf("CommitPaths: %v", err)
		}
		// The committed tree is what matters, not the sha (timestamps differ).
		blob, ok, err := FileBlobAt(dir, headOf(t, dir), "edit.md")
		if err != nil || !ok {
			t.Fatalf("FileBlobAt edit.md: ok=%v err=%v", ok, err)
		}
		if _, ok, _ := FileBlobAt(dir, headOf(t, dir), "gone.md"); ok {
			t.Fatalf("gone.md still in the committed tree")
		}
		left, err := WorktreeChanges(dir)
		if err != nil {
			t.Fatalf("WorktreeChanges: %v", err)
		}
		return string(blob), left
	}

	execBlob, execLeft := commitAndRead(dirtyRepo(t))
	dir := dirtyRepo(t)
	withoutGitBin(t)
	gogitBlob, gogitLeft := commitAndRead(dir)

	if execBlob != "after\n" || gogitBlob != "after\n" {
		t.Fatalf("committed edit.md: exec=%q go-git=%q, want %q", execBlob, gogitBlob, "after\n")
	}
	if len(execLeft) != 0 || len(gogitLeft) != 0 {
		t.Fatalf("worktree not clean after commit: exec=%+v go-git=%+v", execLeft, gogitLeft)
	}
}

func TestCommitPathsNothingToCommitBackendsAgree(t *testing.T) {
	requireGitBin(t)
	_, err := CommitPaths(initRepoWith(t, map[string]string{"a.md": "a\n"}), []string{"a.md"}, "probe")
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("exec clean commit = %v, want ErrNothingToCommit", err)
	}
	dir := initRepoWith(t, map[string]string{"a.md": "a\n"})
	withoutGitBin(t)
	if _, err := CommitPaths(dir, []string{"a.md"}, "probe"); !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("go-git clean commit = %v, want ErrNothingToCommit", err)
	}
}

func TestResetHardBackendsAgree(t *testing.T) {
	requireGitBin(t)
	// Both backends must land on a worktree that matches the tree exactly:
	// the modification reverted, the deletion restored, the untracked file gone
	// (the reconcile loop relies on the reset leaving nothing behind).
	assertClean := func(dir string) {
		t.Helper()
		if err := ResetHard(dir, headOf(t, dir)); err != nil {
			t.Fatalf("ResetHard: %v", err)
		}
		left, err := WorktreeChanges(dir)
		if err != nil {
			t.Fatalf("WorktreeChanges: %v", err)
		}
		if len(left) != 0 {
			t.Fatalf("worktree still dirty after reset: %+v", left)
		}
		if _, err := os.Stat(filepath.Join(dir, "added.md")); !os.IsNotExist(err) {
			t.Fatalf("untracked file survived the reset: %v", err)
		}
	}
	assertClean(dirtyRepo(t))
	dir := dirtyRepo(t)
	withoutGitBin(t)
	assertClean(dir)
}
