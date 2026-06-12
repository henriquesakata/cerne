package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
)

// projCtx is the resolved environment every feature command operates on.
type projCtx struct {
	cfg     project.Config
	projDir string // directory holding .cernerc and docs.lock
	repoDir string // cached backend clone
}

// resolvePaths locates the cerne project from startDir (cwd when empty)
// without touching origin; the cached backend clone is used as-is. Commands
// that must inspect the remote before syncing (push drift detection) start
// here instead of resolveProject.
func resolvePaths(startDir string) (projCtx, error) {
	if startDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return projCtx{}, err
		}
		startDir = wd
	}
	cfg, projDir, err := project.LoadConfig(startDir)
	if err != nil {
		return projCtx{}, err
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		return projCtx{}, err
	}
	repoDir := filepath.Join(cacheDir, "repo")
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); errors.Is(err, os.ErrNotExist) {
		return projCtx{}, fmt.Errorf("cached backend clone missing at %s (run 'cerne init' again to recreate it)", repoDir)
	} else if err != nil {
		return projCtx{}, err
	}
	return projCtx{cfg: cfg, projDir: projDir, repoDir: repoDir}, nil
}

// resolveProject locates the cerne project from startDir (cwd when empty)
// and refreshes the cached backend clone from origin.
func resolveProject(startDir string) (projCtx, error) {
	ctx, err := resolvePaths(startDir)
	if err != nil {
		return projCtx{}, err
	}
	if err := gitback.Pull(ctx.repoDir); err != nil {
		return projCtx{}, fmt.Errorf("refreshing cached backend clone at %s failed (delete that directory and run 'cerne init' to force a fresh clone): %w", ctx.repoDir, err)
	}
	return ctx, nil
}

// updateLockSnapshot rewrites docs.lock with sha as the backend snapshot,
// preserving every other field.
func updateLockSnapshot(projDir, sha string) error {
	lock, err := project.LoadLock(projDir)
	if err != nil {
		return err
	}
	lock.Snapshot = sha
	return project.SaveLock(projDir, lock)
}

// eventActor returns CERNE_ACTOR when set, "local" otherwise.
func eventActor() string {
	if a := os.Getenv("CERNE_ACTOR"); a != "" {
		return a
	}
	return "local"
}
