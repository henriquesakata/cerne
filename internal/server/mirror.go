package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/project"
)

// The Mirror tab's routes (F-0465). The check only lists open issues on each
// tracker, and it is POST because it goes to the network and must not be
// prefetched or cached. The sync (F-0487) is the one that writes: it imports
// outside issues as ideas, like `cerne mirror sync`. None asks the human gate.

func handleMirror(w http.ResponseWriter, repoDir, projDir string) {
	v, err := app.MirrorOverview(repoDir, projDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// handleMirrorCheck spends the user's forge credential, so another origin is
// refused like on every other POST of the local UI.
func handleMirrorCheck(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	if !sameOriginJSON(r) {
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	results, err := app.CheckMirror(repoDir, projDir)
	switch {
	case errors.Is(err, app.ErrMirrorOff):
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	}
}

func handleMirrorCommits(w http.ResponseWriter, projDir, id string) {
	commits, err := app.MirrorCommits(projDir, id)
	switch {
	case errors.Is(err, app.ErrNoGitRepo):
		writeJSON(w, http.StatusOK, map[string]any{"repo": false, "commits": []app.MirrorCommit{}})
	case errors.Is(err, app.ErrBadCardID):
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, map[string]any{"repo": true, "commits": commits})
	}
}

// mirrorSyncLocks holds one lock per backend clone (F-0487), shared by the
// sync route and the panel's loop, so this process never runs two syncs of a
// project at once. A sync from another process (the CLI, someone else's cerne
// ui) is safe anyway: SyncMirror rechecks each issue URL inside the backend lock.
var mirrorSyncLocks sync.Map // repoDir -> *sync.Mutex

func mirrorSyncLock(repoDir string) *sync.Mutex {
	mu, _ := mirrorSyncLocks.LoadOrStore(repoDir, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

var errMirrorSyncRunning = errors.New("a mirror sync is already running for this project")

// trySyncMirror runs one sync, or refuses when one is already running.
func trySyncMirror(repoDir, projDir string) (app.MirrorSyncReport, error) {
	mu := mirrorSyncLock(repoDir)
	if !mu.TryLock() {
		return app.MirrorSyncReport{}, errMirrorSyncRunning
	}
	defer mu.Unlock()
	return app.SyncMirror(repoDir, projDir)
}

// handleMirrorSync is the Sync button of the ideas screen (F-0487). A tracker
// that fails is in its run's errors with a 200, as the CLI prints a warning.
func handleMirrorSync(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	if !sameOriginJSON(r) {
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	rep, err := trySyncMirror(repoDir, projDir)
	switch {
	case errors.Is(err, app.ErrMirrorOff), errors.Is(err, errMirrorSyncRunning):
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		if rep.Ideas == nil {
			rep.Ideas = []string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ideas": rep.Ideas, "trackers": rep.Trackers})
	}
}

// mirrorSyncInterval is how often the panel syncs each project's mirror (F-0487).
const mirrorSyncInterval = 5 * time.Minute

// mirrorSyncLoop syncs the project's mirror at once and then every interval
// while ctx lives, so a new outside issue becomes an idea with nobody running
// `cerne mirror sync`. A round with the mirror off, or with a sync already
// running, does nothing; after runs when a round synced, so the panel can tell
// the open screens to reread the last sync even when no idea came in.
func mirrorSyncLoop(ctx context.Context, repoDir, projDir string, every time.Duration, after func()) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if project.MirrorSettings(projDir).Enabled {
			if _, err := trySyncMirror(repoDir, projDir); err == nil {
				after()
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
