package gitback

import (
	"errors"
	"time"
)

// safePullWindow (F-0200) is how long a sync mark keeps the read refresh quiet.
// Every cerne command routes through resolveProject -> SafePull, and each one
// was paying a full SSH handshake (~1.5s, the connection itself — measured the
// same on a freshly gc'd clone) plus a go-git Status sweep (~0.6s) to relearn
// that the remote had not moved. An agent session fires those in bursts, so the
// window is sized to collapse a burst into one fetch while still refreshing
// inside a minute. It is deliberately NOT the 30s elisionWindow of the reconcile
// path: that one is scaled to the server's 15s pull loop and guards a write
// plan, this one guards a read.
const safePullWindow = 60 * time.Second

// SafePull refreshes workdir from origin without ever using go-git's
// Worktree.Pull: that primitive moves refs/heads/main to the fetched head
// BEFORE its merge reset, which aborts with ErrUnstagedChanges on a dirty
// worktree — leaving the shared clone poisoned (ref at the new head, index and
// files on the old tree), so the next sweep commit silently reverts the remote
// advance (N-0027, the F-0192 plan.md revert). Instead, under the backend
// advisory lock: skip the refresh entirely while the worktree has uncommitted
// writes (a CLI synthesis in flight — the caller proceeds on the cached state),
// otherwise Fetch and hard-reset to the tracking head — ref, index and files
// move together, and only along a fast-forward so an unpushed local commit
// (offline push) is never discarded.
//
// Under a sync mark newer than safePullWindow it skips the network entirely;
// callers that must always see the remote use SafePullNow.
func SafePull(workdir string) error {
	return safePull(workdir, safePullWindow)
}

// SafePullNow is SafePull with the elision disabled — for the callers whose
// whole job is freshness: the server's live-refresh loop (every 15s; letting it
// inherit the window would freeze the UI, the F-0517 regression) and cerne init
// (a one-shot, where skipping a fetch buys nothing).
func SafePullNow(workdir string) error {
	return safePull(workdir, 0)
}

func safePull(workdir string, window time.Duration) error {
	return WithLock(LockPath(workdir), func() error {
		// Fast path: this machine talked to the remote moments ago and the clone
		// already sits on the tracking head, so there is nothing to fetch and
		// nothing to reset — skipping the Status sweep too, which exists only to
		// guard the reset below. A zero window disables it (SafePullNow).
		if window > 0 && freshAndAtTracking(workdir, window) {
			return nil
		}
		changes, err := WorktreeChanges(workdir)
		if err != nil {
			return err
		}
		if len(changes) > 0 {
			return nil
		}
		if err := Fetch(workdir); err != nil {
			return err
		}
		tracking, err := TrackingHeadSHA(workdir)
		if errors.Is(err, ErrNoCommits) {
			return nil // still-empty remote: nothing to pull, the caller seeds it
		}
		if err != nil {
			return err
		}
		head, err := HeadSHA(workdir)
		if errors.Is(err, ErrNoCommits) {
			return ResetHard(workdir, tracking) // unborn clone adopts the remote head
		}
		if err != nil {
			return err
		}
		if head == tracking {
			return nil
		}
		ff, err := IsAncestor(workdir, head, tracking)
		if err != nil || !ff {
			return err
		}
		return ResetHard(workdir, tracking)
	})
}

// freshAndAtTracking reports whether the sync mark is inside window AND the
// clone already sits on the tracking head. Freshness alone is not enough:
// another process may have fetched (advancing the tracking ref) and then
// skipped its own reset on a dirty worktree, leaving this clone's files behind
// a mark that still looks fresh. Any error reading either SHA answers false —
// the full path then decides, exactly as before.
func freshAndAtTracking(workdir string, window time.Duration) bool {
	if !SyncedWithin(workdir, window) {
		return false
	}
	tracking, err := TrackingHeadSHA(workdir)
	if err != nil {
		return false
	}
	head, err := HeadSHA(workdir)
	if err != nil {
		return false
	}
	return head == tracking
}
