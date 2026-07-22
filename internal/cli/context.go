package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gitlab.com/rockt/cerne/internal/app"
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
		// The cache is a derived artifact (clone + index), reconstructible from
		// the committed .cernerc backend. Recreate it instead of dead-ending the
		// user into 'cerne init', which refuses on an initialized project.
		if err := recreateCache(cacheDir, repoDir, cfg.Backend); err != nil {
			return projCtx{}, err
		}
	} else if err != nil {
		return projCtx{}, err
	}
	return projCtx{cfg: cfg, projDir: projDir, repoDir: repoDir}, nil
}

// recreateCache re-clones the backend into repoDir when the cached clone is
// missing. A partial repoDir is wiped first; the notice goes to stderr so it
// never pollutes machine-readable stdout (e.g. `status --json`).
func recreateCache(cacheDir, repoDir, backend string) error {
	if err := os.RemoveAll(repoDir); err != nil {
		return err
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "cerne: clone de cache ausente — recriando de %s...\n", backend)
	if err := gitback.Clone(backend, repoDir); err != nil {
		return fmt.Errorf("recriando o clone de cache de %s falhou: %w", backend, err)
	}
	return nil
}

// resolveProject locates the cerne project from startDir (cwd when empty)
// and refreshes the cached backend clone from origin. SafePull, not Pull — the
// clone is shared with concurrent cerne processes (a ui pull loop, a synthesis
// in flight), and a raw Pull on a dirty worktree poisons it (N-0027: ref
// moved, files left behind); SafePull skips the refresh instead and the
// command runs on the cached state.
func resolveProject(startDir string) (projCtx, error) {
	ctx, err := resolvePaths(startDir)
	if err != nil {
		return projCtx{}, err
	}
	if err := gitback.SafePull(ctx.repoDir); err != nil {
		return projCtx{}, fmt.Errorf("refreshing cached backend clone at %s failed (delete that directory and re-run the command to force a fresh clone): %w", ctx.repoDir, err)
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
	return app.EventActor()
}
