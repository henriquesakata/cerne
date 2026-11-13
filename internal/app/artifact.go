package app

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/index"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/telemetry"
)

// SaveArtifact writes an artifact file under content/artifacts/<id>/ in the
// backend clone (creating the directory when needed), then commits, pushes
// and refreshes the docs.lock snapshot. A byte-identical write leaves the
// worktree clean and is treated as a successful no-op.
func SaveArtifact(repoDir, projDir, id, name string, data []byte) error {
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		dir := filepath.Join(repoDir, "content", "artifacts", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: update artifact %s/%s", id, name), nil
	})
	if errors.Is(err, gitback.ErrNothingToCommit) {
		return nil // backend already had exactly this content
	}
	return err
}

// CommitAndSnapshot lands the backend clone's pending mutation on the latest
// origin/main and pushes it, then rewrites the docs.lock snapshot in projDir to
// the new HEAD. Since F-0122 every app mutation runs through MutateAndSnapshot
// (writes inside the lock) — this remains only for `cerne compile`, whose LLM
// synthesis writes pages as it goes and cannot hold the lock for minutes.
// It routes through the reconcile core, so it holds
// the backend lock (serializing concurrent local processes on the shared clone),
// merges per document, union-merges the append-only event log, and retries on a
// concurrent advance. ErrNothingToCommit when the worktree is clean; a
// *ConflictError when the same document changed locally and remotely. When the
// project opts in (.cernerc autocommit_lock), the snapshot tail also commits the
// freshly-pinned docs.lock.
func CommitAndSnapshot(repoDir, projDir, msg string) error {
	base, err := gitback.HeadSHA(repoDir)
	if errors.Is(err, gitback.ErrNoCommits) {
		base = ""
	} else if err != nil {
		return err
	}
	// Capture the domain op's writes BEFORE the reconcile resets the worktree.
	changes, err := captureChanges(repoDir)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return fmt.Errorf("%w: %s", gitback.ErrNothingToCommit, repoDir)
	}
	_, conflicts, err := ReconcileCommitPush(repoDir, projDir, base, msg, changes)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return &ConflictError{Conflicts: conflicts}
	}
	return nil
}

// MutateAndSnapshot runs a domain mutation and its commit+push as ONE critical
// section of the backend lock (F-0121): reset to a clean HEAD (leftover
// uncommitted state from a crashed process never rides along), run mutate
// (which writes to the worktree and returns the commit message — minted ids may
// be part of it), capture exactly those writes, then reconcile/commit/push via
// the same per-document core as CommitAndSnapshot. Unlike CommitAndSnapshot,
// a concurrent local process can neither sweep this mutation's half-written
// state nor interleave an id mint with it. A failed mutate leaves no partial
// writes behind (best-effort reset). Same tail semantics: ErrNothingToCommit
// when mutate changed nothing, *ConflictError on a same-document collision,
// docs.lock snapshot refreshed on success.
// CONTRACT: mutate may run more than once (re-mint, F-0356: when every conflict
// is on a path it created, it is redone on the new remote tip); it only writes
// into the clone and has no effect outside it (no stdout/stderr, no project
// files, no network, no ledger) and must reset any state it captures.
// It mints its own telemetry Op (kind "backend") so CLI one-shots record
// phases too; the server path uses MutateAndSnapshotOp with the request's Op.
func MutateAndSnapshot(repoDir, projDir string, mutate func() (string, error)) error {
	return mutateAndSnapshot(repoDir, projDir, false, mutate)
}

// mutateAndSnapshotLatest is MutateAndSnapshot on a clone first fast-forwarded
// to origin, so mutate compares against the backend, not a cache that may trail
// it: a mutation that reports ErrNothingToCommit — "unchanged" — is then true of
// origin, not just of this clone (F-0328 critic C3). It costs one fetch.
func mutateAndSnapshotLatest(repoDir, projDir string, mutate func() (string, error)) error {
	return mutateAndSnapshot(repoDir, projDir, true, mutate)
}

func mutateAndSnapshot(repoDir, projDir string, latest bool, mutate func() (string, error)) error {
	op := telemetry.StartOp("backend")
	err := mutateAndSnapshotOp(op, repoDir, projDir, latest, mutate)
	status := "ok"
	if err != nil {
		status = "error"
	}
	op.End(status)
	return err
}

// MutateAndSnapshotOp is MutateAndSnapshot recording its phases (lock_wait,
// reset, mutate, then the reconcile loop's fetch/merge/commit/push, snapshot)
// on the caller's Op (F-0149) — nil-safe, the caller owns End().
func MutateAndSnapshotOp(op *telemetry.Op, repoDir, projDir string, mutate func() (string, error)) error {
	return mutateAndSnapshotOp(op, repoDir, projDir, false, mutate)
}

func mutateAndSnapshotOp(op *telemetry.Op, repoDir, projDir string, latest bool, mutate func() (string, error)) error {
	var head string
	var conflicts []Conflict
	var remints map[string]string
	stopLockWait := op.Phase("lock_wait")
	lockErr := gitback.WithLock(gitback.LockPath(repoDir), func() error {
		stopLockWait()
		base, err := gitback.HeadSHA(repoDir)
		if errors.Is(err, gitback.ErrNoCommits) {
			base = ""
		} else if err != nil {
			return err
		}
		if base != "" {
			stop := op.Phase("reset")
			err := gitback.ResetHard(repoDir, base)
			stop()
			if err != nil {
				return err
			}
		}
		if latest && base != "" {
			if base, err = fastForwardToOrigin(op, repoDir, base); err != nil {
				return err
			}
		}
		// A mint whose id was taken upstream is redone on the new tip (F-0356).
		head, conflicts, remints, err = remintLocked(op, repoDir, base, "", nil, mutate, true, ErrIDRaceLost)
		return err
	})
	if lockErr != nil {
		return lockErr
	}
	if len(conflicts) > 0 {
		return &ConflictError{Conflicts: conflicts}
	}
	if RemintNotice != nil {
		for _, o := range slices.Sorted(maps.Keys(remints)) {
			RemintNotice(o, remints[o])
		}
	}
	if head != "" {
		stop := op.Phase("snapshot")
		err := updateSnapshot(projDir, head)
		stop()
		if err == nil {
			refreshIndex(repoDir, head)
		}
		return err
	}
	return nil
}

// refreshIndex brings the local search index up to head, which a successful
// write has just left equal to the backend: it re-indexes the entities that
// changed since the head the INDEX last recorded (not the project snapshot,
// which a failed refresh would have advanced past the gap): the write itself
// plus whatever the push rebase folded in. With no recorded head, a failing
// diff or no index yet, it rebuilds whole. It runs outside the lock, after the
// write: a failure only reaches IndexNotice, never the caller, and leaves the
// recorded head where it was so the next write heals the gap.
func refreshIndex(repoDir, head string) {
	dbPath := project.IndexPathForRepo(repoDir)
	if old, ok := index.IndexedHead(dbPath); ok {
		if paths, err := gitback.ChangedPaths(repoDir, old, head); err == nil {
			if index.Update(repoDir, dbPath, index.IDsFromPaths(paths), time.Now()) == nil {
				return
			}
		}
	}
	if err := index.Rebuild(repoDir, dbPath, time.Now()); err != nil {
		IndexNotice(err)
	}
}

// ErrIDRaceLost is returned when a re-minted id kept colliding with another
// session's past autoWorkRemintRounds redos.
var ErrIDRaceLost = errors.New("id race lost 3 times; run 'cerne pull' and retry")

// RemintNotice, when set (by the CLI root), is told each id a one-shot command
// minted that was taken upstream, with the id it registered instead, in stable
// order. Called from the shared tail, so the server's MutateAndSnapshotOp path
// reaches it too: the `ui` process logs the line on its own stderr, never in
// the HTTP response.
var RemintNotice func(oldID, newID string)

// IndexNotice is told why the local search index could not be refreshed after
// a write that already reached the backend. The write stands: the index is
// derived and `cerne pull` rebuilds it (F-0394). The CLI root wires it to stderr.
var IndexNotice = func(error) {}

// fastForwardToOrigin fetches and, when origin is ahead of base, moves the
// clone onto it, returning the new base. A clone that diverged from origin
// stays at base: that case belongs to the reconcile loop.
func fastForwardToOrigin(op *telemetry.Op, repoDir, base string) (string, error) {
	stop := op.Phase("fetch")
	err := gitback.Fetch(repoDir)
	stop()
	if err != nil {
		return base, err
	}
	theirs, err := gitback.TrackingHeadSHA(repoDir)
	if errors.Is(err, gitback.ErrNoCommits) || (err == nil && theirs == base) {
		return base, nil
	}
	if err != nil {
		return base, err
	}
	if ahead, err := gitback.IsAncestor(repoDir, base, theirs); err != nil || !ahead {
		return base, err
	}
	if err := gitback.ResetHard(repoDir, theirs); err != nil {
		return base, err
	}
	return theirs, nil
}

// PinAllowed reports whether the committed docs.lock pin may be written in
// projDir: never from normal client operations when pin_writer is "ci";
// otherwise always when the project sets no pin_branch (pre-0102 behavior), or
// only on that branch — feature branches never touch docs.lock, so MRs stop
// conflicting on it (F-0102/F-0198). Shared by the mutation tail
// (updateSnapshot) and `cerne pull`. Best-effort like autocommitLock: an
// unreadable config or a non-git projDir behaves as unrestricted, so a
// misconfiguration never silently stops the pin. A detached HEAD resolves to
// "" and therefore never matches.
func PinAllowed(projDir string) bool {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		return true
	}
	if cfg.PinWriter == "ci" {
		return false
	}
	if cfg.PinBranch == "" {
		return true
	}
	branch, err := gitback.CurrentBranch(projDir)
	if err != nil {
		return true
	}
	return branch == cfg.PinBranch
}

// autocommitLock commits the just-written project-side docs.lock when the
// project opts in via .cernerc (autocommit_lock = true). Best-effort by design:
// a missing/unreadable config, an opt-out, a non-git project, an unchanged lock,
// or pre-staged user changes are all no-ops — the pin is skipped, never failing
// the backend mutation that already succeeded.
func autocommitLock(projDir string) error {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil || !cfg.AutocommitLock {
		return nil
	}
	_, err = gitback.CommitPath(projDir, "docs.lock", "chore(docs): pin docs.lock")
	if errors.Is(err, gitback.ErrNothingToCommit) ||
		errors.Is(err, gitback.ErrNotRepo) ||
		errors.Is(err, gitback.ErrOtherStaged) {
		return nil
	}
	return err
}
