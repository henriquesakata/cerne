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
// every backend mutation (transitions, artifact saves).
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
	return project.SaveLock(projDir, lock)
}
