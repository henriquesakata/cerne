package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
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
