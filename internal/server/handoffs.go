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

// handoffRow is one GET /api/handoffs entry (F-0386) and the GET
// /api/handoffs/{id} payload: the HO- record, its origin work's title ("" when
// that work is gone) and its brief — in the list too, so a queue row opens
// without one request per row (F-0421 crit. 7). The brief leaves out the next
// step and the contingencies, which the record carries as fields.
type handoffRow struct {
	store.Handoff
	FromWorkTitle string `json:"from_work_title"`
	Brief         string `json:"brief"`
}

func newHandoffRow(repoDir string, h store.Handoff, titles map[string]string) (handoffRow, error) {
	content, err := readContentDoc(repoDir, "handoffs", h.ID)
	return handoffRow{h, titles[h.FromWork], app.HandoffBriefBody(repoDir, h, content)}, err
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
		row, err := newHandoffRow(repoDir, h, titles)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
			return
		}
		rows = append(rows, row)
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
	titles, err := app.WorkTitles(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	row, err := newHandoffRow(repoDir, h, titles)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, row)
}
