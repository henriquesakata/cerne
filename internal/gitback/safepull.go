package gitback

import "errors"

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
func SafePull(workdir string) error {
	return WithLock(LockPath(workdir), func() error {
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
