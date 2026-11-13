package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// costsDefaultDays is the costs screen's default period: the last 8 weeks,
// today included.
const costsDefaultDays = 56

// handleCosts serves the costs screen (F-0391): ?since=&until= (YYYY-MM-DD,
// inclusive; default the last 8 weeks) and the level, model, vendor and
// effort filters. Read-only.
func handleCosts(w http.ResponseWriter, r *http.Request, repoDir string) {
	now := time.Now().UTC()
	v := r.URL.Query()
	q := app.CostsQuery{Level: v.Get("level"), Model: v.Get("model"), Vendor: v.Get("vendor"), Effort: v.Get("effort")}
	q.Until = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	q.Since = q.Until.AddDate(0, 0, 1-costsDefaultDays)
	for _, p := range []struct {
		key string
		dst *time.Time
	}{{"since", &q.Since}, {"until", &q.Until}} {
		if s := v.Get(p.key); s != "" {
			d, err := time.Parse(time.DateOnly, s)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, errorBody(p.key+": want a date as YYYY-MM-DD, got "+s))
				return
			}
			*p.dst = d
		}
	}
	if q.Since.After(q.Until) {
		writeJSON(w, http.StatusBadRequest, errorBody("since is after until"))
		return
	}
	rep, err := app.Costs(repoDir, q, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// handleCostDetail serves one feature's stamped cost; an invalid or unknown
// id, or a feature with no stamped cost, is a 404 that says which.
func handleCostDetail(w http.ResponseWriter, repoDir, id string) {
	if !domain.IsFeatureID(id) {
		writeJSON(w, http.StatusNotFound, errorBody("not a feature id: "+id))
		return
	}
	d, err := app.FeatureCost(repoDir, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("feature not found: "+id))
	case errors.Is(err, app.ErrNoCostStamped):
		writeJSON(w, http.StatusNotFound, errorBody(err.Error()))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, d)
	}
}
