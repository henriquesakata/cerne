package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/stats"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestFlowStatsEndpoint: GET /api/stats returns flow metrics (F-0205) computed
// from the event log — lead/cycle time for a completed feature.
func TestFlowStatsEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)
	day := func(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }
	for _, ev := range []domain.Event{
		{ID: "e1", EntityID: "F-0001", Type: "feature.created", At: day(1)},
		{ID: "e2", EntityID: "F-0001", Type: "feature.started", At: day(3)},
		{ID: "e3", EntityID: "F-0001", Type: "feature.completed", At: day(10)},
	} {
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	h := New(repoDir, projDir, "proj")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stats", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var m stats.Metrics
	if err := json.NewDecoder(rec.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(m.Features) != 1 || m.Features[0].ID != "F-0001" {
		t.Fatalf("Features = %+v, want [F-0001]", m.Features)
	}
	if m.Features[0].Lead != 9*24*time.Hour || m.Features[0].Cycle != 7*24*time.Hour {
		t.Fatalf("Lead/Cycle = %v/%v, want 216h/168h", m.Features[0].Lead, m.Features[0].Cycle)
	}
}
