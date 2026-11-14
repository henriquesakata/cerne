package server

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/store"
)

// handoffIDRe is the shape of a handoff id; anything else is a 404 before the
// id reaches a file path.
var handoffIDRe = regexp.MustCompile(`^HO-\d{4,}$`)

// handoffRow is one GET /api/handoffs entry (F-0386): the HO- record plus its
// origin work's title ("" when that work is gone).
type handoffRow struct {
	store.Handoff
	FromWorkTitle string `json:"from_work_title"`
}

// handoffDetail is the GET /api/handoffs/{id} payload: the row plus the brief.
type handoffDetail struct {
	handoffRow
	Brief string `json:"brief"`
}

func handleHandoffs(w http.ResponseWriter, repoDir string) {
	all, err := app.ListHandoffs(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	titles, err := app.WorkTitles(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	rows := make([]handoffRow, 0, len(all))
	for _, h := range all {
		rows = append(rows, handoffRow{h, titles[h.FromWork]})
	}
	writeJSON(w, http.StatusOK, rows)
}

func handleHandoffDetail(w http.ResponseWriter, repoDir, id string) {
	if !handoffIDRe.MatchString(id) {
		writeJSON(w, http.StatusNotFound, errorBody("handoff not found"))
		return
	}
	h, err := store.LoadHandoff(repoDir, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("handoff not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	brief, err := readContentDoc(repoDir, "handoffs", id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	titles, err := app.WorkTitles(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, handoffDetail{handoffRow{h, titles[h.FromWork]}, brief})
}
