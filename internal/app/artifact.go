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
	dir := filepath.Join(repoDir, "content", "artifacts", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return err
	}
	err := CommitAndSnapshot(repoDir, projDir, fmt.Sprintf("cerne: update artifact %s/%s", id, name))
	if errors.Is(err, gitback.ErrNothingToCommit) {
		return nil // backend already had exactly this content
	}
	return err
}

// CommitAndSnapshot commits and pushes the backend clone, then rewrites the
// docs.lock snapshot in projDir to the new HEAD — the sync tail shared by
// every backend mutation (transitions, artifact saves). When the project opts
// in (.cernerc autocommit_lock), it also commits the freshly-pinned docs.lock.
func CommitAndSnapshot(repoDir, projDir, msg string) error {
	sha, err := gitback.CommitAndPush(repoDir, msg)
	if err != nil {
		return err
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
