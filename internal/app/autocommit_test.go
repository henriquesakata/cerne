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

// pinCommitCount walks projDir's log counting docs.lock pin commits.
func pinCommitCount(t *testing.T, projDir string) int {
	t.Helper()
	repo, err := git.PlainOpen(projDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	iter, err := repo.Log(&git.LogOptions{})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	n := 0
	_ = iter.ForEach(func(c *object.Commit) error {
		if c.Message == "chore(docs): pin docs.lock" {
			n++
		}
		return nil
	})
	return n
}

// TestAutocommitAsyncAndCoalesced pins criterion 3 (F-0153): mutations return
// without waiting for the docs.lock pin; a burst of N mutations coalesces to
// far fewer pin commits, and the final pin lands.
func TestAutocommitAsyncAndCoalesced(t *testing.T) {
	repoDir, projDir := newSnapshotBackend(t, true)

	gate := make(chan struct{})
	autocommitHookBeforeCommit = func() { <-gate }
	defer func() { autocommitHookBeforeCommit = nil }()

	const burst = 5
	returned := make(chan struct{}, burst)
	for i := 0; i < burst; i++ {
		content := []byte{'v', byte('0' + i)}
		err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
			p := filepath.Join(repoDir, "content", "notes", "n.md")
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(p, content, 0o644); err != nil {
				return "", err
			}
			return "mutation", nil
		})
		if err != nil {
			t.Fatalf("mutation %d: %v", i, err)
		}
		returned <- struct{}{}
	}
	// All five mutations returned while the first pin commit is still gated:
	// the response path never waited for the autocommit.
	if len(returned) != burst {
		t.Fatalf("only %d/%d mutations returned before the pin was released", len(returned), burst)
	}

	close(gate)
	FlushAutocommits()

	if got := pinCommitCount(t, projDir); got < 1 || got > 2 {
		t.Fatalf("pin commits = %d, want 1-2 (coalesced burst)", got)
	}
	if !projStatusClean(t, projDir) {
		t.Fatal("docs.lock left uncommitted after flush")
	}
}

// newPushPinProject is newSnapshotBackendWithConfig with the projDir cloned
// from its own bare origin (tracking ref present, branch main), so pin
// publication (F-0180) has a remote to push to.
func newPushPinProject(t *testing.T, cfg project.Config) (repoDir, projDir, projOrigin string) {
	t.Helper()
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare backend: %v", err)
	}
	repoDir = filepath.Join(base, "repo")
	if err := gitback.Clone(origin, repoDir); err != nil {
		t.Fatalf("Clone backend: %v", err)
	}
	if err := store.InitLayout(repoDir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed"); err != nil {
		t.Fatalf("CommitAndPush seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "repo", "pending.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write pending: %v", err)
	}

	projOrigin = filepath.Join(base, "proj-origin.git")
	if err := gitback.InitBare(projOrigin); err != nil {
		t.Fatalf("InitBare proj: %v", err)
	}
	projDir = filepath.Join(base, "proj")
	if err := gitback.Clone(projOrigin, projDir); err != nil {
		t.Fatalf("Clone proj: %v", err)
	}
	cfg.Backend = origin
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projDir, ".gitignore"), []byte(".docs/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if err := project.SaveLock(projDir, project.Lock{Version: 1, Backend: origin}); err != nil {
		t.Fatalf("SaveLock initial: %v", err)
	}
	if _, err := gitback.CommitAndPush(projDir, "init proj"); err != nil {
		t.Fatalf("CommitAndPush init proj: %v", err)
	}
	return repoDir, projDir, projOrigin
}

func pushPinConfig() project.Config {
	return project.Config{ProjectID: "demo", AutocommitLock: true, PinBranch: "main", PushPin: true}
}

func mutateOnce(t *testing.T, repoDir, projDir string) {
	t.Helper()
	if err := CommitAndSnapshot(repoDir, projDir, "mutation"); err != nil {
		t.Fatalf("CommitAndSnapshot: %v", err)
	}
}

func bareHeadMessage(t *testing.T, bare string) string {
	t.Helper()
	repo, err := git.PlainOpen(bare)
	if err != nil {
		t.Fatalf("PlainOpen bare: %v", err)
	}
	ref, err := repo.Head()
	if err != nil {
		t.Fatalf("Head bare: %v", err)
	}
	c, err := repo.CommitObject(ref.Hash())
	if err != nil {
		t.Fatalf("CommitObject bare: %v", err)
	}
	return c.Message
}

// Criterion 1 (F-0180): a pin-only branch is published right after the
// deferred pin commit.
func TestPushPinPublishesPinOnlyBranch(t *testing.T) {
	repoDir, projDir, projOrigin := newPushPinProject(t, pushPinConfig())
	mutateOnce(t, repoDir, projDir)
	FlushAutocommits()
	if msg := bareHeadMessage(t, projOrigin); msg != "chore(docs): pin docs.lock" {
		t.Fatalf("origin HEAD message = %q, want the published pin commit", msg)
	}
}

// Criterion 2 (F-0180): an unpublished user commit ahead of origin blocks the
// push — cerne never publishes the user's own work.
func TestPushPinSkipsWhenUserCommitsAhead(t *testing.T) {
	repoDir, projDir, projOrigin := newPushPinProject(t, pushPinConfig())
	if err := os.WriteFile(filepath.Join(projDir, "work.txt"), []byte("user work"), 0o644); err != nil {
		t.Fatalf("write work.txt: %v", err)
	}
	if _, err := gitback.CommitAll(projDir, "user commit"); err != nil {
		t.Fatalf("CommitAll user: %v", err)
	}
	mutateOnce(t, repoDir, projDir)
	FlushAutocommits()
	if got := pinCommitCount(t, projDir); got != 1 {
		t.Fatalf("local pin commits = %d, want 1", got)
	}
	if msg := bareHeadMessage(t, projOrigin); msg != "init proj" {
		t.Fatalf("origin HEAD message = %q, want untouched %q", msg, "init proj")
	}
}

// Criterion 3 (F-0180): an unreachable origin never fails the mutation or the
// autocommit round; the pin stays committed locally.
func TestPushPinBestEffortOnFailure(t *testing.T) {
	repoDir, projDir, _ := newPushPinProject(t, pushPinConfig())
	repo, err := git.PlainOpen(projDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	rcfg, err := repo.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	rcfg.Remotes["origin"].URLs = []string{filepath.Join(t.TempDir(), "nowhere.git")}
	if err := repo.SetConfig(rcfg); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	mutateOnce(t, repoDir, projDir)
	FlushAutocommits()
	if got := pinCommitCount(t, projDir); got != 1 {
		t.Fatalf("local pin commits = %d, want 1 despite unreachable origin", got)
	}
	if !projStatusClean(t, projDir) {
		t.Fatal("docs.lock left uncommitted after failed push round")
	}
}

// Criterion 4 (F-0180): without push_pin the behavior is unchanged — the pin
// is committed locally and never pushed.
func TestPushPinOffByDefault(t *testing.T) {
	cfg := pushPinConfig()
	cfg.PushPin = false
	repoDir, projDir, projOrigin := newPushPinProject(t, cfg)
	mutateOnce(t, repoDir, projDir)
	FlushAutocommits()
	if got := pinCommitCount(t, projDir); got != 1 {
		t.Fatalf("local pin commits = %d, want 1", got)
	}
	if msg := bareHeadMessage(t, projOrigin); msg != "init proj" {
		t.Fatalf("origin HEAD message = %q, want untouched %q (push_pin off)", msg, "init proj")
	}
}

// Criterion 5 (F-0180): a pin committed earlier but never published is pushed
// by a later round even when that round has nothing new to commit.
func TestPushPinSelfHealsOnNothingToCommit(t *testing.T) {
	_, projDir, projOrigin := newPushPinProject(t, pushPinConfig())
	if err := project.SaveLock(projDir, project.Lock{Version: 1, Backend: "x", Snapshot: "deadbeef"}); err != nil {
		t.Fatalf("SaveLock: %v", err)
	}
	if _, err := gitback.CommitPath(projDir, "docs.lock", "chore(docs): pin docs.lock"); err != nil {
		t.Fatalf("CommitPath pin: %v", err)
	}
	enqueueAutocommit(projDir)
	FlushAutocommits()
	if msg := bareHeadMessage(t, projOrigin); msg != "chore(docs): pin docs.lock" {
		t.Fatalf("origin HEAD message = %q, want the self-healed pin push", msg)
	}
}

// TestFlushAutocommitsIdleReturns pins the trivial path: nothing pending.
func TestFlushAutocommitsIdleReturns(t *testing.T) {
	done := make(chan struct{})
	go func() { FlushAutocommits(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("FlushAutocommits hung with nothing pending")
	}
}
