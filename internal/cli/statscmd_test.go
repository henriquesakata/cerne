package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/stats"
	"gitlab.com/rockt/cerne/internal/store"
)

// seedFlowEvents appends feature.created/started/completed events straight to
// the backend's event log (stats is read-only, so bypassing the transition
// gates here is fine — it only needs the log, not valid entity state).
func seedFlowEvents(t *testing.T, repoDir string) {
	t.Helper()
	day := func(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }
	events := []domain.Event{
		{ID: "e1", EntityID: "F-0001", Type: "feature.created", At: day(1)},
		{ID: "e2", EntityID: "F-0001", Type: "feature.started", At: day(3)},
		{ID: "e3", EntityID: "F-0001", Type: "feature.completed", At: day(10)},
	}
	for _, ev := range events {
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
}

func TestFlowStats(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedFlowEvents(t, repoDir)

	out, err := execCerne(t, "stats", "--dir", projDir)
	if err != nil {
		t.Fatalf("stats: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"F-0001", "Lead", "Cycle", "Throughput"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stats output missing %q:\n%s", want, out)
		}
	}
}

func TestFlowStatsJSON(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedFlowEvents(t, repoDir)

	out, err := execCerne(t, "stats", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("stats --json: %v\noutput: %s", err, out)
	}
	var m stats.Metrics
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("stats --json is not valid JSON: %v\n%s", err, out)
	}
	if len(m.Features) != 1 || m.Features[0].ID != "F-0001" {
		t.Fatalf("Features = %+v, want [F-0001]", m.Features)
	}
	if m.Features[0].Lead != 9*24*time.Hour || m.Features[0].Cycle != 7*24*time.Hour {
		t.Fatalf("Lead/Cycle = %v/%v, want 216h/168h", m.Features[0].Lead, m.Features[0].Cycle)
	}
}

func TestFlowStatsSince(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedFlowEvents(t, repoDir)

	out, err := execCerne(t, "stats", "--since", "2026-06-01", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("stats --since: %v\noutput: %s", err, out)
	}
	var m stats.Metrics
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("stats --since --json is not valid JSON: %v\n%s", err, out)
	}
	if len(m.Features) != 0 {
		t.Fatalf("Features = %+v, want none (all completed before --since window)", m.Features)
	}
}

func TestFlowStatsNoWrites(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedFlowEvents(t, repoDir)
	before, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents before: %v", err)
	}

	if _, err := execCerne(t, "stats", "--dir", projDir); err != nil {
		t.Fatalf("stats: %v", err)
	}
	if _, err := execCerne(t, "stats", "--process", "--dir", projDir); err != nil {
		t.Fatalf("stats --process: %v", err)
	}

	after, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents after: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("event log changed: before %d events, after %d — stats must be read-only", len(before), len(after))
	}
}
