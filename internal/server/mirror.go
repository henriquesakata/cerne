package server

import (
	"errors"
	"net/http"

	"github.com/henriquesakata/cerne/internal/app"
)

// The Mirror tab's routes (F-0465). All read: the check only lists open issues
// on each tracker, and it is POST because it goes to the network and must not
// be prefetched or cached. None asks the human gate; none writes.

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
