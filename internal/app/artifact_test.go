package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// newSnapshotBackend sets up a backend clone (with a dirty change ready to push)
// plus a projDir that is its OWN git repo holding a tracked docs.lock and a
// .cernerc with autocommit_lock set to `autocommit`.
func newSnapshotBackend(t *testing.T, autocommit bool) (repoDir, projDir string) {
	t.Helper()
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	repoDir = filepath.Join(base, "repo")
	if err := gitback.Clone(origin, repoDir); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := store.InitLayout(repoDir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed"); err != nil {
		t.Fatalf("CommitAndPush seed: %v", err)
	}
	// A pending backend change so the next CommitAndSnapshot has something to push.
	if err := os.WriteFile(filepath.Join(repoDir, "pending.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	projDir = filepath.Join(base, "proj")
	repo, err := git.PlainInit(projDir, false)
	if err != nil {
		t.Fatalf("PlainInit proj: %v", err)
	}
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "demo", Backend: origin, AutocommitLock: autocommit}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := project.SaveLock(projDir, project.Lock{Version: 1, Backend: origin}); err != nil {
		t.Fatalf("SaveLock initial: %v", err)
	}
	wt, _ := repo.Worktree()
	if _, err := wt.Add(".cernerc"); err != nil {
		t.Fatalf("add .cernerc: %v", err)
	}
	if _, err := wt.Add("docs.lock"); err != nil {
		t.Fatalf("add docs.lock: %v", err)
	}
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	if _, err := wt.Commit("init proj", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("commit init proj: %v", err)
	}
	return repoDir, projDir
}

func projStatusClean(t *testing.T, projDir string) bool {
	t.Helper()
	repo, _ := git.PlainOpen(projDir)
	wt, _ := repo.Worktree()
	st, err := wt.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	return st.IsClean()
}

func projHeadMessage(t *testing.T, projDir string) string {
	t.Helper()
	repo, _ := git.PlainOpen(projDir)
	ref, _ := repo.Head()
	c, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject: %v", err)
	}
	return c.Message
}

func TestCommitAndSnapshotAutocommitOn(t *testing.T) {
	repoDir, projDir := newSnapshotBackend(t, true)
	if err := CommitAndSnapshot(repoDir, projDir, "mutation"); err != nil {
		t.Fatalf("CommitAndSnapshot: %v", err)
	}
	if !projStatusClean(t, projDir) {
		t.Fatalf("projDir worktree not clean — docs.lock left uncommitted with autocommit on")
	}
	if msg := projHeadMessage(t, projDir); msg != "chore(docs): pin docs.lock" {
		t.Fatalf("HEAD message = %q, want the pin commit", msg)
	}
}

func TestCommitAndSnapshotAutocommitOff(t *testing.T) {
	repoDir, projDir := newSnapshotBackend(t, false)
	if err := CommitAndSnapshot(repoDir, projDir, "mutation"); err != nil {
		t.Fatalf("CommitAndSnapshot: %v", err)
	}
	if projStatusClean(t, projDir) {
		t.Fatalf("projDir clean — docs.lock should be left dirty when autocommit off")
	}
	if msg := projHeadMessage(t, projDir); msg != "init proj" {
		t.Fatalf("HEAD advanced to %q — no pin expected with autocommit off", msg)
	}
}
