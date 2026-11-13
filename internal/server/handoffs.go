package server

import (
	"cmp"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
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

// closedAt is when a closed handoff closed: taken_at, else updated_at.
func closedAt(h store.Handoff) time.Time {
	if h.TakenAt != nil {
		return *h.TakenAt
	}
	return h.UpdatedAt
}

// handoffOrder sorts pending first (oldest first), then the closed ones by
// closing time, newest first (F-0386 crit. 2).
func handoffOrder(a, b store.Handoff) int {
	ap, bp := a.State == domain.HandoffPending, b.State == domain.HandoffPending
	switch {
	case ap && bp:
		return a.CreatedAt.Compare(b.CreatedAt)
	case ap:
		return -1
	case bp:
		return 1
	}
	return cmp.Compare(closedAt(b).UnixNano(), closedAt(a).UnixNano())
}

func workTitles(repoDir string) (map[string]string, error) {
	works, err := store.LoadAllWorks(repoDir)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(works))
	for _, w := range works {
		m[w.ID] = w.Title
	}
	return m, nil
}

func handleHandoffs(w http.ResponseWriter, repoDir string) {
	all, err := store.LoadAllHandoffs(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	titles, err := workTitles(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	slices.SortStableFunc(all, handoffOrder)
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
	titles, err := workTitles(repoDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, handoffDetail{handoffRow{h, titles[h.FromWork]}, brief})
}
