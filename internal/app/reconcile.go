package app

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
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
var ErrTooConcurrent = errors.New("backend too concurrent: could not sync after several attempts")

// Reconcile retry tuning (tests lower these).
var (
	reconcileMaxAttempts = 6
	reconcileBackoff     = 40 * time.Millisecond
)

// reconcileHookBeforePush, when non-nil, runs immediately before each push
// attempt. It exists for tests to inject a concurrent remote advance and
// exercise the non-fast-forward retry path; production leaves it nil.
var reconcileHookBeforePush func(attempt int)

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
	var head string
	var conflicts []Conflict
	lockErr := gitback.WithLock(gitback.LockPath(repoDir), func() error {
		// Start from a clean base tree: discards any uncommitted caller writes
		// (already captured in `changes`) so the merge is fully deterministic.
		if base != "" {
			if err := gitback.ResetHard(repoDir, base); err != nil {
				return err
			}
		}
		for attempt := 0; attempt < reconcileMaxAttempts; attempt++ {
			if err := gitback.Fetch(repoDir); err != nil {
				return err
			}
			theirs, err := gitback.TrackingHeadSHA(repoDir)
			if errors.Is(err, gitback.ErrNoCommits) {
				theirs = "" // empty remote: nothing to merge against
			} else if err != nil {
				return err
			}

			apply, conf, err := plan3Way(repoDir, base, theirs, changes)
			if err != nil {
				return err
			}
			if len(conf) > 0 {
				conflicts = conf // non-destructive: tree is still at base
				return nil
			}
			if len(apply) == 0 {
				// Every change is already present remotely (or a no-op): realign
				// the worktree and snapshot to theirs without pushing.
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
			sha, err := gitback.CommitAll(repoDir, msg)
			if errors.Is(err, gitback.ErrNothingToCommit) {
				head = theirs // merged content equals theirs byte-for-byte
				return nil
			}
			if err != nil {
				return err
			}
			if reconcileHookBeforePush != nil {
				reconcileHookBeforePush(attempt)
			}
			pushErr := gitback.Push(repoDir)
			if gitback.IsNonFastForward(pushErr) {
				// Remote advanced between fetch and push: rewind and retry.
				if base != "" {
					if err := gitback.ResetHard(repoDir, base); err != nil {
						return err
					}
				}
				time.Sleep(reconcileBackoff * time.Duration(attempt+1))
				continue
			}
			if pushErr != nil {
				return pushErr
			}
			head = sha
			return nil
		}
		return ErrTooConcurrent
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
				conflicts = append(conflicts, Conflict{Path: ch.Path, Diff: "remoto alterou um documento que você removeu localmente"})
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
	return autocommitLock(projDir)
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
