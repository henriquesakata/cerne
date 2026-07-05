package gitback

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
)

// writeNested writes rel (slash-separated, possibly nested) under dir,
// creating parent directories — backend layouts are nested, unlike the flat
// files of writeFile.
func writeNested(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCommitPathsCommitsOnlyDeclared(t *testing.T) {
	dir := initRepoWith(t, map[string]string{"spec.md": "base\n"})
	// The op's declared writes: a new entity file and a modified spec.
	writeNested(t, dir, "entities/features/F-0002.json", "{\"id\":\"F-0002\"}\n")
	writeFile(t, dir, "spec.md", "edited\n")
	// A concurrent process's half-written state, present in the shared worktree.
	writeNested(t, dir, "entities/features/F-0099.json", "{\"id\":\"F-0099\"}\n")

	sha, err := CommitPaths(dir, []string{
		"entities/features/F-0002.json",
		"spec.md",
	}, "cerne: create F-0002")
	if err != nil {
		t.Fatalf("CommitPaths: %v", err)
	}
	if !shaHex.MatchString(sha) {
		t.Fatalf("sha = %q, want 40-char hex", sha)
	}
	repo, _ := git.PlainOpen(dir)
	headRef, _ := repo.Head()
	head, _ := repo.CommitObject(headRef.Hash())
	parent, _ := head.Parent(0)
	patch, _ := parent.Patch(head)
	var touched []string
	for _, fp := range patch.FilePatches() {
		from, to := fp.Files()
		if to != nil {
			touched = append(touched, to.Path())
		} else if from != nil {
			touched = append(touched, from.Path())
		}
	}
	if len(touched) != 2 {
		t.Fatalf("commit touched %v, want exactly the 2 declared paths", touched)
	}
	// The stranger's file stays untracked on disk — never swept into the commit.
	st, _ := func() (git.Status, error) { wt, _ := repo.Worktree(); return wt.Status() }()
	if got := st.File("entities/features/F-0099.json").Worktree; got != git.Untracked {
		t.Fatalf("stranger worktree status = %v, want Untracked", got)
	}
	if got := st.File("entities/features/F-0099.json").Staging; got != git.Untracked {
		t.Fatalf("stranger staging status = %v, want Untracked (not staged)", got)
	}
}

func TestCommitPathsStagesDeletion(t *testing.T) {
	dir := initRepoWith(t, map[string]string{"a.md": "x\n", "b.md": "y\n"})
	if err := os.Remove(filepath.Join(dir, "a.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitPaths(dir, []string{"a.md"}, "rm a"); err != nil {
		t.Fatalf("CommitPaths deletion: %v", err)
	}
	repo, _ := git.PlainOpen(dir)
	headRef, _ := repo.Head()
	head, _ := repo.CommitObject(headRef.Hash())
	if _, err := head.File("a.md"); err == nil {
		t.Fatalf("a.md still present in HEAD after deletion commit")
	}
	if _, err := head.File("b.md"); err != nil {
		t.Fatalf("b.md missing from HEAD: %v", err)
	}
}

func TestCommitPathsNothingToCommit(t *testing.T) {
	dir := initRepoWith(t, map[string]string{"a.md": "x\n"})
	before := headOf(t, dir)
	_, err := CommitPaths(dir, []string{"a.md"}, "noop")
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("err = %v, want ErrNothingToCommit", err)
	}
	if headOf(t, dir) != before {
		t.Fatalf("HEAD advanced on a no-op")
	}
}

// TestCommitPathsRaceWindowStranger pins the exact repro of F-0121: a stranger
// file lands in the shared worktree between staging and the status check (the
// window where CommitAll's IsClean guard used to pass and go-git's raw
// ErrEmptyCommit escaped). The declared paths are unchanged, so the outcome
// must be the ErrNothingToCommit sentinel — never go-git's generic error, and
// never a commit containing the stranger.
func TestCommitPathsRaceWindowStranger(t *testing.T) {
	dir := initRepoWith(t, map[string]string{"a.md": "x\n"})
	before := headOf(t, dir)
	commitPathsHookAfterAdd = func() {
		writeFile(t, dir, "stranger.md", "concurrent write\n")
	}
	defer func() { commitPathsHookAfterAdd = nil }()

	_, err := CommitPaths(dir, []string{"a.md"}, "noop under race")
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("err = %v, want ErrNothingToCommit (raw go-git empty-commit must not escape)", err)
	}
	if headOf(t, dir) != before {
		t.Fatalf("HEAD advanced: stranger swept into a commit")
	}
}
