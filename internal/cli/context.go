package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
)

// projCtx is the resolved environment every feature command operates on.
type projCtx struct {
	cfg     project.Config
	projDir string // directory holding .cernerc and docs.lock
	repoDir string // cached backend clone
}

// lastProject is the project the latest resolvePaths picked: the root's
// PersistentPostRun names it when the command wrote to the backend (F-0347).
var lastProject struct{ id, backend, cernerc string }

// writeNotice tracks the F-0347 line for the running command: the pushes
// count at its start (a delta, so several commands in one process stay exact)
// and whether the line already went out.
var writeNotice struct {
	pushes0 int64
	printed bool
}

func startWriteNotice() {
	writeNotice.pushes0, writeNotice.printed = gitback.Pushes(), false
	lastProject.id = ""
}

// NoteWriteAfterError is for main: cobra skips PersistentPostRun when a command
// fails, and a write followed by an error is when the destination matters most.
// Safe after success too: the line goes out once.
func NoteWriteAfterError(w io.Writer) { noteWrite(w) }

// noteWrite prints, once, where the command's write landed. push/sync call it
// before their final line (F-0876 wants that one last); the root's
// PersistentPostRun covers every other command.
func noteWrite(w io.Writer) {
	if writeNotice.printed || gitback.Pushes() <= writeNotice.pushes0 || lastProject.id == "" {
		return
	}
	writeNotice.printed = true
	fmt.Fprintf(w, "cerne: wrote to project %q (%s) — .cernerc at %s\n",
		lastProject.id, lastProject.backend, lastProject.cernerc)
}

// backendForDisplay drops the userinfo, query and fragment of a URL backend so
// a token embedded in it never reaches stderr. It fails to the safe side: no
// url.Parse (it rejects, and so would echo, a malformed escape) and no regex;
// everything up to the LAST "@" after "://" goes, so a token holding a raw "@"
// or "/" cannot leave a piece behind (an "@" legitimately in the path costs the
// host: over-redaction, accepted). The scp form (git@host:path) and local paths
// have no "://" and stay as written.
func backendForDisplay(backend string) string {
	scheme, rest, ok := strings.Cut(backend, "://")
	if !ok {
		return backend
	}
	// Userinfo first, over the whole rest: a "?" or "#" inside a token must not
	// end the search for its "@" (over-redaction on an "@" in the query is safe).
	rest = rest[strings.LastIndex(rest, "@")+1:]
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		rest = rest[:i]
	}
	return scheme + "://" + rest
}

// resolvePaths locates the cerne project from startDir (cwd when empty)
// without touching origin; the cached backend clone is used as-is. Commands
// that must inspect the remote before syncing (push drift detection) start
// here instead of resolveProject.
func resolvePaths(startDir string) (projCtx, error) {
	var cfg project.Config
	var projDir string
	var err error
	if startDir != "" { // from --dir: it names the project, no climbing (F-0354)
		cfg, projDir, err = project.LoadConfigAt(startDir)
	} else {
		var wd string
		if wd, err = os.Getwd(); err != nil {
			return projCtx{}, err
		}
		startDir = wd
		cfg, projDir, err = project.LoadConfig(startDir)
	}
	if err != nil {
		return projCtx{}, err
	}
	noteCerneRoot(startDir, projDir)
	lastProject.id, lastProject.backend = cfg.ProjectID, backendForDisplay(cfg.Backend)
	lastProject.cernerc = filepath.Join(projDir, ".cernerc")
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

// refuseProjectlessDir is the root pre-run check of F-0354: a --dir the user
// passed names the project directory, so a folder without .cernerc is refused
// before the command reads or writes anything, whether the command resolves
// the project through resolvePaths or on its own (msg, room, hook, settings…).
// init is exempt: it creates the .cernerc. A default value is not "passed".
func refuseProjectlessDir(cmd *cobra.Command) error {
	f := cmd.Flag("dir")
	if f == nil || !f.Changed || cmd.Name() == "init" {
		return nil
	}
	_, _, err := project.LoadConfigAt(f.Value.String())
	return err
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
