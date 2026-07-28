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
	return newSnapshotBackendWithConfig(t, project.Config{ProjectID: "demo", AutocommitLock: autocommit})
}

// newSnapshotBackendWithConfig is newSnapshotBackend with the full .cernerc
// under test control (cfg.Backend is filled in with the origin). The projDir
// repo starts on go-git's init default branch, "master".
func newSnapshotBackendWithConfig(t *testing.T, cfg project.Config) (repoDir, projDir string) {
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
	cfg.Backend = origin
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	// Real projects gitignore .docs/ (cerne init writes this); without it the
	// session snapshot sidecar would show up as untracked and break the
	// worktree-clean assertions.
	if err := os.WriteFile(filepath.Join(projDir, ".gitignore"), []byte(".docs/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if err := project.SaveLock(projDir, project.Lock{Version: 1, Backend: origin}); err != nil {
		t.Fatalf("SaveLock initial: %v", err)
	}
	wt, _ := repo.Worktree()
	if _, err := wt.Add(".cernerc"); err != nil {
		t.Fatalf("add .cernerc: %v", err)
	}
	if _, err := wt.Add(".gitignore"); err != nil {
		t.Fatalf("add .gitignore: %v", err)
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
	FlushAutocommits() // the pin is deferred (F-0153); land it before asserting
	if !projStatusClean(t, projDir) {
		t.Fatalf("projDir worktree not clean — docs.lock left uncommitted with autocommit on")
	}
	if msg := projHeadMessage(t, projDir); msg != "chore(docs): pin docs.lock" {
		t.Fatalf("HEAD message = %q, want the pin commit", msg)
	}
}

// Criterion 2 (F-0102): with pin_branch set and the project on another branch,
// a backend mutation leaves docs.lock untouched — no rewrite, no commit.
func TestCommitAndSnapshotPinBranchOtherBranch(t *testing.T) {
	repoDir, projDir := newSnapshotBackendWithConfig(t,
		project.Config{ProjectID: "demo", AutocommitLock: true, PinBranch: "main"})
	// projDir sits on go-git's init default "master" ≠ pin branch "main".
	before, err := os.ReadFile(filepath.Join(projDir, "docs.lock"))
	if err != nil {
		t.Fatalf("read docs.lock before: %v", err)
	}
	if err := CommitAndSnapshot(repoDir, projDir, "mutation"); err != nil {
		t.Fatalf("CommitAndSnapshot: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(projDir, "docs.lock"))
	if err != nil {
		t.Fatalf("read docs.lock after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("docs.lock rewritten off the pin branch:\nbefore: %s\nafter: %s", before, after)
	}
	if !projStatusClean(t, projDir) {
		t.Fatalf("projDir worktree dirty — nothing should be written off the pin branch")
	}
	if msg := projHeadMessage(t, projDir); msg != "init proj" {
		t.Fatalf("HEAD advanced to %q — no pin commit expected off the pin branch", msg)
	}
	// Criterion 4: the session sidecar still tracks the new backend HEAD.
	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	got, err := project.LoadSessionSnapshot(projDir)
	if err != nil {
		t.Fatalf("LoadSessionSnapshot: %v", err)
	}
	if got != head {
		t.Fatalf("session snapshot = %q, want backend HEAD %q", got, head)
	}
}

// Criterion 3 (F-0102): on the pin branch the behavior is today's — docs.lock
// rewritten and (autocommit_lock on) committed.
func TestCommitAndSnapshotPinBranchCurrent(t *testing.T) {
	repoDir, projDir := newSnapshotBackendWithConfig(t,
		project.Config{ProjectID: "demo", AutocommitLock: true, PinBranch: "master"})
	if err := CommitAndSnapshot(repoDir, projDir, "mutation"); err != nil {
		t.Fatalf("CommitAndSnapshot: %v", err)
	}
	FlushAutocommits() // the pin is deferred (F-0153); land it before asserting
	if !projStatusClean(t, projDir) {
		t.Fatalf("projDir worktree not clean — docs.lock left uncommitted on the pin branch")
	}
	if msg := projHeadMessage(t, projDir); msg != "chore(docs): pin docs.lock" {
		t.Fatalf("HEAD message = %q, want the pin commit", msg)
	}
	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if lock.Snapshot != head {
		t.Fatalf("lock.Snapshot = %q, want backend HEAD %q", lock.Snapshot, head)
	}
}

func TestCommitAndSnapshotCIWriterLeavesLockUntouched(t *testing.T) {
	repoDir, projDir := newSnapshotBackendWithConfig(t,
		project.Config{
			ProjectID:      "demo",
			AutocommitLock: true,
			PushPin:        true,
			PinWriter:      "ci",
		})
	before, err := os.ReadFile(filepath.Join(projDir, "docs.lock"))
	if err != nil {
		t.Fatalf("read docs.lock before: %v", err)
	}
	if err := CommitAndSnapshot(repoDir, projDir, "mutation"); err != nil {
		t.Fatalf("CommitAndSnapshot: %v", err)
	}
	FlushAutocommits()
	after, err := os.ReadFile(filepath.Join(projDir, "docs.lock"))
	if err != nil {
		t.Fatalf("read docs.lock after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("docs.lock rewritten by a client in CI writer mode:\nbefore: %s\nafter: %s", before, after)
	}
	if msg := projHeadMessage(t, projDir); msg != "init proj" {
		t.Fatalf("HEAD advanced to %q — no local pin commit expected in CI writer mode", msg)
	}
	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	got, err := project.LoadSessionSnapshot(projDir)
	if err != nil {
		t.Fatalf("LoadSessionSnapshot: %v", err)
	}
	if got != head {
		t.Fatalf("session snapshot = %q, want backend HEAD %q", got, head)
	}
}

// Criterion 4 (F-0102): every mutation records the session snapshot sidecar,
// pin_branch or not.
func TestCommitAndSnapshotWritesSessionSnapshot(t *testing.T) {
	repoDir, projDir := newSnapshotBackend(t, false)
	if err := CommitAndSnapshot(repoDir, projDir, "mutation"); err != nil {
		t.Fatalf("CommitAndSnapshot: %v", err)
	}
	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	got, err := project.LoadSessionSnapshot(projDir)
	if err != nil {
		t.Fatalf("LoadSessionSnapshot: %v", err)
	}
	if got != head {
		t.Fatalf("session snapshot = %q, want backend HEAD %q", got, head)
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
