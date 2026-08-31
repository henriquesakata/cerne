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

// hostileRepo is dirtyRepo's nastier sibling: the shapes that actually break a
// porcelain parser — a name with an accent (git quotes it octally unless -z), a
// name with a space, an untracked file nested in a NEW directory (plain
// --porcelain collapses that to "dir/" and lists nothing inside), and an
// ignored file (neither backend may ever report it).
func hostileRepo(t *testing.T) string {
	t.Helper()
	dir := initRepoWith(t, map[string]string{
		".gitignore":      "ignored/\n*.tmp\n",
		"ação usuário.md": "antes\n",
	})
	writeFile(t, dir, "ação usuário.md", "depois\n")
	for _, rel := range []string{"nova pasta/aninhado.md", "ignored/nope.md"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeFile(t, dir, rel, "x\n")
	}
	writeFile(t, dir, "scratch.tmp", "x\n")
	return dir
}

func TestWorktreeChangesHostileNamesBackendsAgree(t *testing.T) {
	requireGitBin(t)
	exec, err := WorktreeChanges(hostileRepo(t))
	if err != nil {
		t.Fatalf("exec WorktreeChanges: %v", err)
	}
	dir := hostileRepo(t)
	withoutGitBin(t)
	gogit, err := WorktreeChanges(dir)
	if err != nil {
		t.Fatalf("go-git WorktreeChanges: %v", err)
	}
	want := []Change{{Path: "ação usuário.md"}, {Path: "nova pasta/aninhado.md"}}
	if !reflect.DeepEqual(gogit, want) {
		t.Fatalf("go-git changes = %+v, want %+v", gogit, want)
	}
	if !reflect.DeepEqual(exec, gogit) {
		t.Fatalf("exec changes = %+v, want %+v", exec, gogit)
	}
}

func TestCommitPathsHostileNamesBackendsAgree(t *testing.T) {
	requireGitBin(t)
	paths := []string{"ação usuário.md", "nova pasta/aninhado.md"}
	commit := func(dir string) string {
		t.Helper()
		if _, err := CommitPaths(dir, paths, "probe"); err != nil {
			t.Fatalf("CommitPaths: %v", err)
		}
		blob, ok, err := FileBlobAt(dir, headOf(t, dir), "nova pasta/aninhado.md")
		if err != nil || !ok {
			t.Fatalf("nested path not committed: ok=%v err=%v", ok, err)
		}
		left, err := WorktreeChanges(dir)
		if err != nil {
			t.Fatalf("WorktreeChanges: %v", err)
		}
		if len(left) != 0 {
			t.Fatalf("worktree not clean after commit: %+v", left)
		}
		return string(blob)
	}
	execBlob := commit(hostileRepo(t))
	dir := hostileRepo(t)
	withoutGitBin(t)
	if gogitBlob := commit(dir); gogitBlob != execBlob {
		t.Fatalf("committed blob: exec=%q go-git=%q", execBlob, gogitBlob)
	}
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
