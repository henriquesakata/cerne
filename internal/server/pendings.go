package server

import (
	"errors"
	"net/http"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/store"
)

// Pendings API (F-0334): read-only, the same selection as cerne pending list
// (state, from, component and module, as its flags).

func handlePendings(w http.ResponseWriter, r *http.Request, repoDir string) {
	q := r.URL.Query()
	pendings, err := app.ListPendings(repoDir, app.PendingFilter{State: q.Get("state"), From: q.Get("from"), Component: q.Get("component"), Module: q.Get("module")})
	if errors.Is(err, app.ErrPendingStateInvalid) {
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Pendings []store.Pending `json:"pendings"`
	}{pendings})
}
