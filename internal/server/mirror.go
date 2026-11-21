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

// trySyncMirror runs one sync (full: cards too; otherwise only the import),
// or refuses when one is already running.
func trySyncMirror(repoDir, projDir string, full bool) (app.MirrorSyncReport, error) {
	mu := mirrorSyncLock(repoDir)
	if !mu.TryLock() {
		return app.MirrorSyncReport{}, errMirrorSyncRunning
	}
	defer mu.Unlock()
	if full {
		return app.SyncMirror(repoDir, projDir)
	}
	return app.ImportMirror(repoDir, projDir)
}

// handleMirrorSync is the Sync button of the ideas screen (F-0487). A tracker
// that fails is in its run's errors with a 200, as the CLI prints a warning.
func handleMirrorSync(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	if !sameOriginJSON(r) {
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	rep, err := trySyncMirror(repoDir, projDir, true)
	switch {
	// reason tells the two 409s apart without the client reading the message.
	case errors.Is(err, app.ErrMirrorOff):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "reason": "off"})
	case errors.Is(err, errMirrorSyncRunning):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "reason": "running"})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		if rep.Ideas == nil {
			rep.Ideas = []string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"ideas": rep.Ideas, "trackers": rep.Trackers})
	}
}

// How often the panel looks at each project's mirror (F-0487): new outside
// issues every 15 s (one list call per tracker, ~240 an hour), and the full
// sync, which also brings each mirrored card's issue to its state, every 5 min.
const (
	mirrorImportInterval = 15 * time.Second
	mirrorSyncInterval   = 5 * time.Minute
)

// mirrorSyncLoop syncs the project's mirror at once, then only imports new
// outside issues every interval and runs the full sync again once full has
// passed, while ctx lives: a new outside issue becomes an idea within seconds,
// with nobody running `cerne mirror sync`. A round with the mirror off, or with
// a sync already running, does nothing (a full round skipped is retried on the
// next tick). after runs when a full round synced or a round imported ideas, so
// the panel tells the open screens to reread, without waking them every tick.
func mirrorSyncLoop(ctx context.Context, repoDir, projDir string, every, full time.Duration, after func()) {
	t := time.NewTicker(every)
	defer t.Stop()
	var lastFull time.Time
	for {
		if project.MirrorSettings(projDir).Enabled {
			isFull := lastFull.IsZero() || time.Since(lastFull) >= full
			if rep, err := trySyncMirror(repoDir, projDir, isFull); err == nil {
				if isFull {
					lastFull = time.Now()
				}
				if isFull || len(rep.Ideas) > 0 {
					after()
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
