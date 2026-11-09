package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	noteCerneRoot(startDir, projDir)
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

// noteCerneRoot says on stderr when the project was found through CERNE_ROOT
// and not by walking up from startDir (F-0876), so adopting the variable is
// never silent. A startDir inside projDir was found by the walk: no note.
func noteCerneRoot(startDir, projDir string) {
	env := os.Getenv("CERNE_ROOT")
	if env == "" {
		return
	}
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return
	}
	if rel, err := filepath.Rel(projDir, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}
	fmt.Fprintf(os.Stderr, "cerne: no project from %s; using CERNE_ROOT=%s\n", abs, projDir)
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
	fmt.Fprintf(os.Stderr, "cerne: cache clone missing — re-cloning from %s...\n", backend)
	if err := gitback.Clone(backend, repoDir); err != nil {
		return fmt.Errorf("re-cloning the cache clone from %s failed: %w", backend, err)
	}
	return nil
}

// resolveProject locates the cerne project from startDir (cwd when empty)
// and refreshes the cached backend clone from origin. SafePull, not Pull — the
// clone is shared with concurrent cerne processes (a ui pull loop, a synthesis
// in flight), and a raw Pull on a dirty worktree poisons it (N-0027: ref
// moved, files left behind); SafePull skips the refresh instead and the
// command runs on the cached state.
// SafePull, not SafePullNow: this is the read refresh, and it is on the path of
// every command, so it takes the F-0200 elision — a burst of reads in one agent
// session collapses to a single handshake.
func resolveProject(startDir string) (projCtx, error) {
	return resolveRefreshed(startDir, gitback.SafePull)
}

// resolveProjectNow is resolveProject for commands that must not run on a
// possibly-stale clone: they publish a durable artifact from the backend head
// (lock pin), so silently succeeding against an unreachable backend would pin a
// snapshot nobody verified.
func resolveProjectNow(startDir string) (projCtx, error) {
	return resolveRefreshed(startDir, gitback.SafePullNow)
}

func resolveRefreshed(startDir string, refresh func(string) error) (projCtx, error) {
	ctx, err := resolvePaths(startDir)
	if err != nil {
		return projCtx{}, err
	}
	if err := refresh(ctx.repoDir); err != nil {
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
