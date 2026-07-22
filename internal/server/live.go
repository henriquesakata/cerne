package server

import (
	"context"
	"errors"
	"time"

	"gitlab.com/rockt/cerne/internal/gitback"
)

// headPollInterval is how often the watcher reads the backend clone's HEAD.
// One ref read per second per running `cerne ui` is negligible.
const headPollInterval = time.Second

// keepaliveInterval bounds idle time on an SSE connection so proxies and the
// browser keep it open between real change events.
const keepaliveInterval = 25 * time.Second

// pullLoop fast-forwards the backend clone from origin every interval so a
// teammate's push reaches the dashboard. It never talks to the hub: a pull that
// brings commits advances HEAD, which watchHeadMulti then notices — one
// notification path for local and remote changes alike. Pull errors (offline,
// auth) are ignored so the dashboard keeps serving the cached clone.
func pullLoop(ctx context.Context, repoDir string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = pullTick(repoDir)
		}
	}
}

// pullTick is one iteration of pullLoop. It never uses go-git's Worktree.Pull:
// that primitive moves refs/heads/main to the fetched head BEFORE its merge
// reset, which aborts with ErrUnstagedChanges on a dirty worktree — leaving the
// shared clone poisoned (ref at the new head, index and files on the old tree),
// so the next sweep commit silently reverts the remote advance (N-0027, the
// F-0192 plan.md revert). Instead, under the backend lock: skip the tick while
// the worktree has uncommitted writes (a CLI synthesis in flight), otherwise
// Fetch and hard-reset to the tracking head — ref, index and files move
// together, and only along a fast-forward so an unpushed local commit (offline
// push) is never discarded.
func pullTick(repoDir string) error {
	return gitback.WithLock(gitback.LockPath(repoDir), func() error {
		changes, err := gitback.WorktreeChanges(repoDir)
		if err != nil {
			return err
		}
		if len(changes) > 0 {
			return nil
		}
		if err := gitback.Fetch(repoDir); err != nil {
			return err
		}
		tracking, err := gitback.TrackingHeadSHA(repoDir)
		if err != nil {
			return err
		}
		head, err := gitback.HeadSHA(repoDir)
		if errors.Is(err, gitback.ErrNoCommits) {
			return gitback.ResetHard(repoDir, tracking) // unborn clone adopts the remote head
		}
		if err != nil {
			return err
		}
		if head == tracking {
			return nil
		}
		ff, err := gitback.IsAncestor(repoDir, head, tracking)
		if err != nil || !ff {
			return err
		}
		return gitback.ResetHard(repoDir, tracking)
	})
}
