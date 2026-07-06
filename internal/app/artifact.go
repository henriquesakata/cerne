package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
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
func MutateAndSnapshot(repoDir, projDir string, mutate func() (string, error)) error {
	var head string
	var conflicts []Conflict
	lockErr := gitback.WithLock(gitback.LockPath(repoDir), func() error {
		base, err := gitback.HeadSHA(repoDir)
		if errors.Is(err, gitback.ErrNoCommits) {
			base = ""
		} else if err != nil {
			return err
		}
		if base != "" {
			if err := gitback.ResetHard(repoDir, base); err != nil {
				return err
			}
		}
		msg, err := mutate()
		if err != nil {
			if base != "" {
				_ = gitback.ResetHard(repoDir, base) // best-effort: drop partial writes
			}
			return err
		}
		changes, err := captureChanges(repoDir)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			return fmt.Errorf("%w: %s", gitback.ErrNothingToCommit, repoDir)
		}
		head, conflicts, err = reconcileLocked(repoDir, base, msg, changes)
		return err
	})
	if lockErr != nil {
		return lockErr
	}
	if len(conflicts) > 0 {
		return &ConflictError{Conflicts: conflicts}
	}
	if head != "" {
		return updateSnapshot(projDir, head)
	}
	return nil
}

// PinAllowed reports whether the committed docs.lock pin may be written in
// projDir: always when the project sets no pin_branch (pre-0102 behavior),
// otherwise only on that branch — feature branches never touch docs.lock, so
// MRs stop conflicting on it (F-0102). Shared by the mutation tail
// (updateSnapshot) and `cerne pull`. Best-effort like autocommitLock: an
// unreadable config or a non-git projDir behaves as unrestricted, so a
// misconfiguration never silently stops the pin. A detached HEAD resolves to
// "" and therefore never matches.
func PinAllowed(projDir string) bool {
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil || cfg.PinBranch == "" {
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
