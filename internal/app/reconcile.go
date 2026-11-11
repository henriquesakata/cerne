package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/telemetry"
)

// FileChange is one backend-relative document the caller wants to land:
// Content is the desired whole-file body (ignored when Deleted).
type FileChange struct {
	Path    string // slash-separated, relative to the backend clone root
	Content []byte
	Deleted bool
}

// Conflict names a document whose change collides with a different remote change
// to the same document, with a short diff of mine vs. theirs.
type Conflict struct {
	Path string
	Diff string
}

// ErrConflict is the sentinel a ConflictError matches via errors.Is.
var ErrConflict = errors.New("document conflict")

// ConflictError carries the per-document conflicts from a backend mutation that
// could not be reconciled (the same document changed both locally and remotely).
// Nothing was committed; the conflicts name each document and a diff.
type ConflictError struct {
	Conflicts []Conflict
}

func (e *ConflictError) Error() string {
	paths := make([]string, len(e.Conflicts))
	for i, c := range e.Conflicts {
		paths[i] = c.Path
	}
	return fmt.Sprintf("conflito em %d documento(s): %s", len(e.Conflicts), strings.Join(paths, ", "))
}

func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// captureChanges reads the current uncommitted worktree mutation (a domain op's
// entity JSON, appended events and content) into in-memory changes, so the
// reconcile loop can re-apply it onto the latest remote.
func captureChanges(repoDir string) ([]FileChange, error) {
	raw, err := gitback.WorktreeChanges(repoDir)
	if err != nil {
		return nil, err
	}
	changes := make([]FileChange, 0, len(raw))
	for _, c := range raw {
		if c.Deleted {
			changes = append(changes, FileChange{Path: c.Path, Deleted: true})
			continue
		}
		content, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(c.Path)))
		if err != nil {
			return nil, err
		}
		changes = append(changes, FileChange{Path: c.Path, Content: content})
	}
	return changes, nil
}

// ErrTooConcurrent is the sentinel for a reconcile that kept losing the push
// race past reconcileMaxAttempts.
// ErrUnconfirmedHead is the sentinel for the backstop of F-0285: a reconcile
// exit produced a head that was never seen on the remote during this
// operation. It is unreachable through the exits that exist today — each
// confirms its own head — and exists so that a NEW exit returning a head
// without pushing fails loudly instead of quietly pinning a stale snapshot
// into docs.lock, where only comparing two clones would ever reveal it.
var ErrUnconfirmedHead = errors.New("reconcile: head was never confirmed against the remote")

var ErrTooConcurrent = errors.New("backend too concurrent: could not sync after several attempts")

// Reconcile retry tuning (tests lower these).
var (
	reconcileMaxAttempts = 6
	reconcileBackoff     = 40 * time.Millisecond
	// elisionWindow (F-0156): attempt 0 skips its fetch when this process
	// synced the backend's remote this recently — 2× the server's periodic
	// pull, so the mark never ages past two cycles. Retries always fetch.
	elisionWindow = 30 * time.Second
)

// reconcileHookBeforePush, when non-nil, runs immediately before each push
// attempt. It exists for tests to inject a concurrent remote advance and
// exercise the non-fast-forward retry path; production leaves it nil.
var reconcileHookBeforePush func(attempt int)

// reconcileHookAfterWrite, when non-nil, runs between materializing the merged
// changes and committing them. It exists for tests to inject a concurrent
// process's write into the shared worktree and pin that the commit covers only
// the applied paths (F-0121); production leaves it nil.
var reconcileHookAfterWrite func()

// ReconcileCommitPush brings the shared backend clone to the latest origin/main
// and applies changes as a per-document 3-way merge over base (the commit the
// changes were authored against), then commits and pushes — retrying with
// backoff on a concurrent non-fast-forward advance. It returns the new pushed
// head (empty when nothing needed pushing), or a non-empty []Conflict with
// nothing committed when a change collides with a different remote change to the
// SAME document. The whole operation holds the backend lock so concurrent local
// processes serialize on the shared clone. Disjoint documents never conflict;
// append-only event logs are union-merged.
func ReconcileCommitPush(repoDir, projDir, base, msg string, changes []FileChange) (string, []Conflict, error) {
	return ReconcileCommitPushMutate(repoDir, projDir, base, msg, changes, nil)
}

// ReconcileCommitPushMutate is ReconcileCommitPush with an optional mutate
// step run inside the same lock, right after resetting to base and before the
// reconcile loop: it may write directly into repoDir's worktree (e.g. a
// pickup auto-work, F-0208 criterion 1), and those writes are captured and
// merged into changes alongside the caller's. A nil mutate makes this
// identical to ReconcileCommitPush.
func ReconcileCommitPushMutate(repoDir, projDir, base, msg string, changes []FileChange, mutate func() error) (string, []Conflict, error) {
	var head string
	var conflicts []Conflict
	lockErr := gitback.WithLock(gitback.LockPath(repoDir), func() error {
		// Start from a clean base tree: discards any uncommitted caller writes
		// (already captured in `changes`) so the merge is fully deterministic.
		if err := resetToBase(repoDir, base); err != nil {
			return err
		}
		if mutate != nil {
			if err := mutate(); err != nil {
				return err
			}
			extra, err := captureChanges(repoDir)
			if err != nil {
				return err
			}
			changes = append(changes, extra...)
		}
		var err error
		head, conflicts, err = reconcileLocked(nil, repoDir, base, msg, changes)
		return err
	})
	if lockErr != nil {
		return "", nil, lockErr
	}
	if len(conflicts) > 0 {
		return "", conflicts, nil
	}
	if head != "" {
		if err := updateSnapshot(projDir, head); err != nil {
			return "", nil, err
		}
	}
	return head, nil, nil
}

// reconcileLocked is the fetch/merge/commit/push retry loop of
// ReconcileCommitPush, extracted so MutateAndSnapshot can run a domain
// mutation and this loop inside ONE critical section of the backend lock
// (F-0121). The caller must hold the lock. Phases and non-fast-forward
// retries are recorded on op (nil-safe, F-0149) — a retried attempt repeats
// its phase names in order, so a slow op shows exactly where each round went.
func reconcileLocked(op *telemetry.Op, repoDir, base, msg string, changes []FileChange) (string, []Conflict, error) {
	var head string
	var conflicts []Conflict
	err := func() (retErr error) {
		// Every failure below can return with the shared clone mutated — reset
		// to theirs, merged changes written, even a commit made. The next
		// holder of the backend lock inherits this worktree, so hand it back at
		// base: the state each attempt of this loop already assumes, and the
		// one the caller reset to before calling. A reset that itself fails is
		// joined onto the original error, never swallowed (F-0285).
		// headConfirmed answers, at the loop's single exit, the only question
		// that matters for the pin: was this head seen on the remote during
		// THIS operation? It is set where the answer is actually produced — a
		// real fetch, a confirmation fetch, a successful push — never at the
		// exits that consume it. That is the structural half of F-0285: the
		// three exits that return a head without pushing confirm it
		// themselves, but a fourth added later would inherit false here and
		// fail loudly instead of silently pinning a stale snapshot.
		headConfirmed := false
		defer func() {
			if retErr == nil {
				retErr = unconfirmedHead(head, headConfirmed)
			}
			if retErr == nil {
				return
			}
			if rErr := resetToBase(repoDir, base); rErr != nil {
				retErr = errors.Join(retErr, rErr)
			}
		}()
		// confirmed records that this operation already paid its one
		// confirmation fetch, so the tracking ref is trustworthy from here on:
		// a replan must not re-elide (it would plan against a ref it never
		// checked) nor confirm again (the ref was just fetched).
		confirmed := false
		// mustReplan is the ONE gate every exit that returns a head WITHOUT
		// pushing passes through, written once and closed over the loop's state:
		// three copies of it is three chances to copy it subtly wrong, and a
		// wrongly-copied confirmation is the one mistake the backstop above
		// cannot catch — it would set headConfirmed just the same. True means
		// the plan was made against a ref that has since moved and must be
		// remade; false means theirs is confirmed and the exit may record it.
		mustReplan := func(theirs string, elided bool) (bool, error) {
			if !elided {
				return false, nil
			}
			advanced, err := confirmElidedHead(op, repoDir, theirs)
			if err != nil {
				return false, err
			}
			// The fetch happened either way: the ref is now this operation's,
			// confirmed whether or not it moved. Recording that only on an
			// advance would let a later planning round elide against a ref it
			// then re-confirms — harmless today only because every caller
			// returns immediately, which is a property of the callers, not of
			// this gate.
			headConfirmed = true
			confirmed = true
			return advanced, nil
		}
		for attempt := 0; attempt < reconcileMaxAttempts; attempt++ {
			// A planning round is not a push attempt. An elided plan that turns
			// out to be stale is re-made here, against the confirmed head,
			// without consuming one of reconcileMaxAttempts — nothing was
			// pushed, so there is no concurrency to back off from, and the
			// confirmed flag caps this inner loop at one extra round (F-0285).
		plan:
			for {
				elided := attempt == 0 && !confirmed && gitback.SyncedWithin(repoDir, elisionWindow)
				// A real fetch below IS the confirmation; an elided one is not.
				headConfirmed = !elided
				if elided {
					// F-0156: this process talked to the remote moments ago (the
					// server pulls every 15s), so attempt 0 plans against the
					// tracking head it already has. When that turns out stale, the
					// correction is the non-fast-forward retry below, which fetches
					// and replans — but it only fires on a push. All three exits
					// that return WITHOUT pushing (a conflict, nothing to apply,
					// nothing to commit) are outside its reach and confirm the head
					// themselves via confirmElidedHead (F-0285).
					op.Phase("fetch_skip")()
				} else {
					stop := op.Phase("fetch")
					err := gitback.Fetch(repoDir)
					stop()
					if err != nil {
						return err
					}
				}
				theirs, err := gitback.TrackingHeadSHA(repoDir)
				if errors.Is(err, gitback.ErrNoCommits) {
					theirs = "" // empty remote: nothing to merge against
				} else if err != nil {
					return err
				}

				stop := op.Phase("merge")
				apply, conf, err := plan3Way(repoDir, base, theirs, changes)
				stop()
				if err != nil {
					return err
				}
				if len(conf) > 0 {
					// A conflict is reported to the caller and stashed as "the
					// remote version": an elided, stale theirs would manufacture a
					// conflict against content that no longer exists remotely, and
					// nothing here pushes, so confirm before believing it (F-0285).
					redo, cErr := mustReplan(theirs, elided)
					if cErr != nil {
						return cErr
					}
					if redo {
						continue plan
					}
					conflicts = conf // non-destructive: tree is still at base
					return nil
				}
				if len(apply) == 0 {
					// Every change is already present remotely (or a no-op): realign
					// the worktree and snapshot to theirs without pushing. Nothing
					// is pushed here, so an elided plan gets its one confirmation
					// now — otherwise the stale tracking SHA becomes the pin (F-0285).
					redo, cErr := mustReplan(theirs, elided)
					if cErr != nil {
						return cErr
					}
					if redo {
						continue plan
					}
					if theirs != "" && theirs != base {
						if err := gitback.ResetHard(repoDir, theirs); err != nil {
							return err
						}
					}
					head = theirs
					return nil
				}

				// Land on the latest remote, re-apply the merged changes, commit.
				if theirs != "" {
					if err := gitback.ResetHard(repoDir, theirs); err != nil {
						return err
					}
				}
				if err := writeChanges(repoDir, apply); err != nil {
					return err
				}
				if reconcileHookAfterWrite != nil {
					reconcileHookAfterWrite()
				}
				// Commit exactly the applied paths: a concurrent process's writes in
				// the shared worktree must never be swept into this commit (F-0121).
				paths := make([]string, len(apply))
				for i, ch := range apply {
					paths[i] = ch.Path
				}
				stop = op.Phase("commit")
				sha, err := gitback.CommitPaths(repoDir, paths, msg)
				stop()
				if errors.Is(err, gitback.ErrNothingToCommit) {
					// Same hazard as the empty plan above: this exit never pushes,
					// so the retry cannot correct an elided head (F-0285).
					redo, cErr := mustReplan(theirs, elided)
					if cErr != nil {
						return cErr
					}
					if redo {
						// Unlike the exits above, this one already reset to theirs and
						// wrote the merged changes; the replan assumes a tree at base.
						if rErr := resetToBase(repoDir, base); rErr != nil {
							return rErr
						}
						continue plan
					}
					head = theirs // merged content equals theirs byte-for-byte
					return nil
				}
				if err != nil {
					return err
				}
				if reconcileHookBeforePush != nil {
					reconcileHookBeforePush(attempt)
				}
				stop = op.Phase("push")
				pushErr := gitback.Push(repoDir)
				stop()
				if gitback.IsNonFastForward(pushErr) {
					// Remote advanced between fetch and push: rewind and retry.
					// break, not continue: this IS a push attempt and must consume
					// one of reconcileMaxAttempts, unlike a confirmation replan.
					op.AddRetry()
					if err := resetToBase(repoDir, base); err != nil {
						return err
					}
					time.Sleep(reconcileBackoff * time.Duration(attempt+1))
					break plan
				}
				if pushErr != nil {
					return pushErr
				}
				// The push is a remote round trip that the remote accepted:
				// this sha is on origin by construction.
				headConfirmed = true
				head = sha
				return nil
			}
		}
		return ErrTooConcurrent
	}()
	if err != nil {
		// A failed reconcile has no head to record: returning one beside an
		// error invites a caller that checks the value before the error to
		// pin it.
		return "", nil, err
	}
	return head, conflicts, nil
}

// unconfirmedHead is the backstop's predicate, kept separate so it is pinned
// by a test rather than only by the exits that happen to exist. A head is
// recordable only when this operation saw it on the remote; an empty head
// records nothing and needs no confirmation.
// resetToBase returns the shared worktree to the commit this operation was
// planned against. An empty base is not a waiver but the absence of a target:
// it means the backend had no commits when the caller started (a freshly
// initialized backend), so there is no tree to restore and the caller's own
// entry reset skipped it for the same reason. Anything written into such a
// worktree is uncommitted and is discarded by the next operation's reset, or
// swept into nothing — never into a commit, which only ever names paths from
// the plan.
func resetToBase(repoDir, base string) error {
	if base == "" {
		return nil
	}
	return gitback.ResetHard(repoDir, base)
}

func unconfirmedHead(head string, confirmed bool) error {
	if head == "" || confirmed {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrUnconfirmedHead, head)
}

// confirmElidedHead re-fetches and reports whether the remote moved past
// theirs. It backs the two reconcile exits that return a head without ever
// pushing: their SHA comes from the tracking ref, and when attempt 0 elided
// its fetch (F-0156) that ref may predate the real remote head — which would
// then be written to .docs/.snapshot and docs.lock as the project's pin, with
// no error and no retry to correct it (F-0285). An advance means the plan was
// made against stale blobs, so the caller replans.
func confirmElidedHead(op *telemetry.Op, repoDir, theirs string) (bool, error) {
	stop := op.Phase("fetch_confirm")
	err := gitback.Fetch(repoDir)
	stop()
	if err != nil {
		return false, err
	}
	fresh, err := gitback.TrackingHeadSHA(repoDir)
	if errors.Is(err, gitback.ErrNoCommits) {
		fresh = "" // empty remote: nothing advanced
	} else if err != nil {
		return false, err
	}
	return fresh != theirs, nil
}

// plan3Way decides, per change, what to apply given the base and remote (theirs)
// blobs. Disjoint documents take mine; append-only logs union-merge; a same-doc
// divergence is a conflict (nothing is written here — pure git reads).
func plan3Way(repoDir, base, theirs string, changes []FileChange) (apply []FileChange, conflicts []Conflict, err error) {
	for _, ch := range changes {
		var baseBlob []byte
		if base != "" {
			baseBlob, _, err = gitback.FileBlobAt(repoDir, base, ch.Path)
			if err != nil {
				return nil, nil, err
			}
		}
		var theirsBlob []byte
		var theirsExists bool
		if theirs != "" {
			theirsBlob, theirsExists, err = gitback.FileBlobAt(repoDir, theirs, ch.Path)
			if err != nil {
				return nil, nil, err
			}
		}
		switch {
		case isAppendOnly(ch.Path):
			apply = append(apply, FileChange{Path: ch.Path, Content: unionMergeNDJSON(baseBlob, ch.Content, theirsBlob)})
		case ch.Deleted:
			switch {
			case !theirsExists: // already gone remotely
			case bytes.Equal(theirsBlob, baseBlob):
				apply = append(apply, FileChange{Path: ch.Path, Deleted: true})
			default:
				conflicts = append(conflicts, Conflict{Path: ch.Path, Diff: "remote changed a document you removed locally"})
			}
		default: // whole-file write
			switch {
			case theirsExists && bytes.Equal(ch.Content, theirsBlob): // already there
			case bytes.Equal(theirsBlob, baseBlob): // remote untouched: take mine
				apply = append(apply, FileChange{Path: ch.Path, Content: ch.Content})
			case bytes.Equal(ch.Content, baseBlob): // I did not change it: keep theirs
			default:
				conflicts = append(conflicts, Conflict{Path: ch.Path, Diff: shortDiff(ch.Content, theirsBlob)})
			}
		}
	}
	return apply, conflicts, nil
}

// isAppendOnly reports whether path is a partitioned, append-only event log,
// which must be union-merged rather than treated as a whole-file conflict.
func isAppendOnly(path string) bool {
	return strings.HasPrefix(path, "events/") && strings.HasSuffix(path, ".ndjson")
}

// writeChanges materializes apply into the backend clone (writing or deleting).
func writeChanges(repoDir string, apply []FileChange) error {
	for _, ch := range apply {
		dst := filepath.Join(repoDir, filepath.FromSlash(ch.Path))
		if ch.Deleted {
			if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, ch.Content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// updateSnapshot records the new backend snapshot in projDir (the shared tail
// of every backend mutation): always in the gitignored session sidecar, and in
// the committed docs.lock pin — committed too when the project opts in — only
// where pinAllowed says so (off the pin branch, docs.lock stays untouched;
// F-0102).
func updateSnapshot(projDir, sha string) error {
	if err := project.SaveSessionSnapshot(projDir, sha); err != nil {
		return err
	}
	// Record the user project's HEAD as the drift baseline (F-0113); inert on a
	// non-git project.
	if err := RecordProjectHead(projDir); err != nil {
		return err
	}
	if !PinAllowed(projDir) {
		return nil
	}
	lock, err := project.LoadLock(projDir)
	if err != nil {
		return err
	}
	lock.Snapshot = sha
	if err := project.SaveLock(projDir, lock); err != nil {
		return err
	}
	// The pin commit is deferred and coalesced (F-0153) — it used to run go-git
	// Status over the user's whole worktree inside the request, the measured
	// 113s-average snapshot phase of W-0003.
	enqueueAutocommit(projDir)
	return nil
}

// shortDiff renders a compact diff of mine vs. theirs (common prefix/suffix
// trimmed), so a real same-document conflict is distinguishable from a false
// snapshot collision. Capped to keep terminal output readable.
func shortDiff(mine, theirs []byte) string {
	const cap = 30
	m := strings.Split(strings.TrimRight(string(mine), "\n"), "\n")
	th := strings.Split(strings.TrimRight(string(theirs), "\n"), "\n")
	p := 0
	for p < len(m) && p < len(th) && m[p] == th[p] {
		p++
	}
	s := 0
	for s < len(m)-p && s < len(th)-p && m[len(m)-1-s] == th[len(th)-1-s] {
		s++
	}
	var b strings.Builder
	b.WriteString("--- remoto\n+++ meu\n")
	writeSide := func(prefix string, lines []string) {
		n := 0
		for i := p; i < len(lines)-s; i++ {
			if n == cap {
				fmt.Fprintf(&b, "%s …\n", prefix)
				break
			}
			fmt.Fprintf(&b, "%s %s\n", prefix, lines[i])
			n++
		}
	}
	writeSide("-", th)
	writeSide("+", m)
	return b.String()
}

// unionMergeNDJSON merges three versions of an append-only, newline-delimited
// log (e.g. events/<YYYY-MM>.ndjson) by union: every distinct non-empty line
// from base, ours and theirs, in first-occurrence order. Append-only logs only
// grow, so concurrent appends commute — union keeps both sides' new lines
// without the false conflict a whole-file 3-way would raise. The result ends
// with a trailing newline when non-empty, nil when empty.
func unionMergeNDJSON(base, ours, theirs []byte) []byte {
	seen := make(map[string]bool)
	var lines []string
	for _, src := range [][]byte{base, ours, theirs} {
		for _, ln := range strings.Split(string(src), "\n") {
			if ln == "" || seen[ln] {
				continue
			}
			seen[ln] = true
			lines = append(lines, ln)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}
