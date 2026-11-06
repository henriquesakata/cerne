package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/cost"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/stats"
	"gitlab.com/rockt/cerne/internal/store"
)

// seedProcessPerf stages a process+metric marker and n deliveries after it,
// stamped when stamp is true.
func seedProcessPerf(t *testing.T, repoDir string, n int, stamp bool) {
	t.Helper()
	day := func(d int) time.Time { return time.Date(2026, 3, d, 12, 0, 0, 0, time.UTC) }
	if err := store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "Mudança", State: domain.DecisionActive,
		Facets: []string{"process"}, Components: []string{"metric"}, CreatedAt: day(2)}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	for i := range n {
		id := "F-000" + string(rune('1'+i))
		if err := store.AppendEvent(repoDir, domain.Event{ID: "ev" + id, EntityID: id, Type: "feature.completed", At: day(3 + i)}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
		body := "# " + id + "\n"
		if stamp {
			var err error
			if body, err = cost.Stamp(body, cost.Report{Counters: cost.Counters{Input: 1000 * int64(i+1)}, Messages: 1}); err != nil {
				t.Fatalf("Stamp: %v", err)
			}
		}
		dir := filepath.Join(repoDir, "content", "features", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "result.md"), []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
}

func getProcessPerf(t *testing.T, repoDir, projDir string) (stats.ProcessReport, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	New(repoDir, projDir, "proj").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/process-perf", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	var r stats.ProcessReport
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return r, raw
}

// TestProcessPerfEndpoint: GET /api/process-perf serves the process panel
// (F-0321) — the marker and the weekly series.
func TestProcessPerfEndpoint(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedProcessPerf(t, repoDir, 2, true)

	r, _ := getProcessPerf(t, repoDir, projDir)
	if len(r.Markers) != 1 || r.Markers[0].ID != "D-0001" || len(r.Weeks) != 1 {
		t.Fatalf("report = %+v, want marker D-0001 and one week", r)
	}
}

// TestProcessPerfEndpointNoAnchors pins criterion 16 at the API: no stamped
// delivery is a 200 with the explicit reason and weeks: [].
func TestProcessPerfEndpointNoAnchors(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedProcessPerf(t, repoDir, 1, false)

	r, raw := getProcessPerf(t, repoDir, projDir)
	if !strings.Contains(r.Reason, "F-0309") || !regexp.MustCompile(`"weeks":\s*\[\]`).MatchString(raw) {
		t.Fatalf("body = %s, want the F-0309 reason and weeks: []", raw)
	}
}
