package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/cost"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/stats"
	"gitlab.com/rockt/cerne/internal/store"
)

// The panel is read-only, so its fixtures are a bare layout, no git.
func newPanelBackend(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "Demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	return repoDir
}

var panelDay0 = time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC) // a Monday

func panelAt(day int) time.Time { return panelDay0.AddDate(0, 0, day) }

// stamped is body with a real "## Custo" anchor carrying counters.
func stamped(t *testing.T, body string, c cost.Counters) string {
	t.Helper()
	out, err := cost.Stamp(body, cost.Report{Counters: c, Messages: 1})
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	return out
}

// seedDelivered writes an implemented feature, its feature.completed event at
// completed and — when result is not "-" — its result.md.
func seedDelivered(t *testing.T, repoDir, id string, completed time.Time, result string) {
	t.Helper()
	f := store.Feature{ID: id, Title: "Feature " + id, State: domain.StateImplemented, CreatedAt: completed.AddDate(0, 0, -3)}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	ev := domain.NewEvent(id, "feature.completed", nil, "test")
	ev.At = completed
	if err := store.AppendEvent(repoDir, ev); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if result == "-" {
		return
	}
	dir := filepath.Join(repoDir, "content", "features", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "result.md"), []byte(result), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func seedDecision(t *testing.T, repoDir, id string, at time.Time, state domain.DecisionState, facets, components []string) {
	t.Helper()
	d := store.Decision{ID: id, Title: "Decisão " + id, State: state, Facets: facets, Components: components, CreatedAt: at, UpdatedAt: at}
	if err := store.SaveDecision(repoDir, d); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
}

func mustProcessPerf(t *testing.T, repoDir string) stats.ProcessReport {
	t.Helper()
	r, err := ProcessPerf(repoDir, panelAt(1000))
	if err != nil {
		t.Fatalf("ProcessPerf: %v", err)
	}
	return r
}

// TestProcessPerfExcludesUnstampedFeature pins criteria 1-2 in the collector:
// a delivery is sampled by its anchor's own counters, dated by its FIRST
// feature.completed; no result, a result without an anchor or an all-zero
// anchor is excluded and counted.
func TestProcessPerfExcludesUnstampedFeature(t *testing.T) {
	repoDir := newPanelBackend(t)
	seedDelivered(t, repoDir, "F-0001", panelAt(0), stamped(t, "# F-0001\n", cost.Counters{Input: 302, CacheCreation: 339857, CacheRead: 28934595, Output: 150077}))
	again := domain.NewEvent("F-0001", "feature.completed", nil, "test")
	again.At = panelAt(14) // a later re-completion never re-dates the delivery
	if err := store.AppendEvent(repoDir, again); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	seedDelivered(t, repoDir, "F-0002", panelAt(1), "# F-0002\n\n## Resultado\n\nfeito\n")
	seedDelivered(t, repoDir, "F-0003", panelAt(2), stamped(t, "# F-0003\n", cost.Counters{}))
	seedDelivered(t, repoDir, "F-0004", panelAt(3), "-")

	r := mustProcessPerf(t, repoDir)
	if r.ExcludedNoCost != 3 {
		t.Fatalf("excluded_no_cost = %d, want 3", r.ExcludedNoCost)
	}
	if len(r.Weeks) != 1 || r.Weeks[0].N != 1 || r.Weeks[0].Median != 4068967.75 || r.Weeks[0].Week != "2026-W10" {
		t.Fatalf("weeks = %+v, want one 2026-W10 week, n 1, median 4068967.75", r.Weeks)
	}
}

// TestProcessPerfGauntletGuard pins criteria 4-5 in the collector: defeats
// come from the result's Gauntlet record; an exemption or no section is out of
// the returns guard and counted, never zero returns.
func TestProcessPerfGauntletGuard(t *testing.T) {
	repoDir := newPanelBackend(t)
	seedDecision(t, repoDir, "D-0001", panelAt(0), domain.DecisionActive, []string{"process"}, []string{"metric"})
	c := cost.Counters{Input: 1000}
	seedDelivered(t, repoDir, "F-0001", panelAt(1), stamped(t, "## Gauntlet\n\nBarra: x\nRodada 1 — derrota: \"a\"\nRodada 2 — derrota: \"b\"\nRodada 3 — vitória: \"c\"\n", c))
	seedDelivered(t, repoDir, "F-0002", panelAt(2), stamped(t, "## Gauntlet\n\nBarra: x\nRodada 1 — vitória: \"a\"\n", c))
	seedDelivered(t, repoDir, "F-0003", panelAt(3), stamped(t, "## Gauntlet\n\nBarra: não aplicável (sem barra comparável)\n", c))
	seedDelivered(t, repoDir, "F-0004", panelAt(4), stamped(t, "## Resultado\n\nfeito\n", c))

	r := mustProcessPerf(t, repoDir)
	a := r.Markers[0].After
	if a.N != 4 || a.WithGauntlet != 2 || a.ReturnedShare != 0.5 || r.ExcludedNoGauntlet != 2 {
		t.Fatalf("after = n %d with %d returned %v, excluded_no_gauntlet %d, want 4 / 2 / 0.5 / 2",
			a.N, a.WithGauntlet, a.ReturnedShare, r.ExcludedNoGauntlet)
	}
}

// TestProcessPerfMarkersNeedProcessAndMetric pins criterion 6: a marker is a
// decision with facet process AND component metric, in any state, dated by
// its created_at.
func TestProcessPerfMarkersNeedProcessAndMetric(t *testing.T) {
	repoDir := newPanelBackend(t)
	seedDecision(t, repoDir, "D-0001", panelAt(0), domain.DecisionActive, []string{"process"}, nil)
	seedDecision(t, repoDir, "D-0002", panelAt(5), domain.DecisionSuperseded, []string{"process"}, []string{"metric"})
	seedDecision(t, repoDir, "D-0003", panelAt(9), domain.DecisionActive, nil, []string{"metric"})

	r := mustProcessPerf(t, repoDir)
	if len(r.Markers) != 1 || r.Markers[0].ID != "D-0002" || !r.Markers[0].At.Equal(panelAt(5)) {
		t.Fatalf("markers = %+v, want only D-0002 at its created_at", r.Markers)
	}
}

// TestProcessPerfEscapedFromFixes pins criterion 19 in the collector: a
// feature whose fixes names a delivery, created within 28 days after it, is
// that delivery's escaped defect.
func TestProcessPerfEscapedFromFixes(t *testing.T) {
	repoDir := newPanelBackend(t)
	seedDecision(t, repoDir, "D-0001", panelAt(0), domain.DecisionActive, []string{"process"}, []string{"metric"})
	seedDelivered(t, repoDir, "F-0001", panelAt(1), stamped(t, "# F-0001\n", cost.Counters{Input: 1000}))
	fix := store.Feature{ID: "F-0002", Title: "Corrige", Fixes: "F-0001", State: domain.StateCandidate, CreatedAt: panelAt(6)}
	if err := store.SaveFeature(repoDir, fix); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}

	r := mustProcessPerf(t, repoDir)
	if a := r.Markers[0].After; a.N != 1 || a.EscapedShare == nil || *a.EscapedShare != 1 {
		t.Fatalf("after = n %d escaped %v, want 1 / 1", a.N, a.EscapedShare)
	}
}

// TestProcessPerfNoAnchorsReason pins criterion 16 in the collector: with no
// stamped delivery the panel says why, with an empty series — never an error.
func TestProcessPerfNoAnchorsReason(t *testing.T) {
	repoDir := newPanelBackend(t)
	seedDelivered(t, repoDir, "F-0001", panelAt(1), "# F-0001\n")

	r := mustProcessPerf(t, repoDir)
	if !strings.Contains(r.Reason, "F-0309") || r.Weeks == nil || len(r.Weeks) != 0 || r.ExcludedNoCost != 1 {
		t.Fatalf("report = %+v, want the F-0309 reason, empty weeks, excluded 1", r)
	}
}

// TestProcessPerfUnreadableResultErrors: a result that exists but cannot be
// read is an error, never a silent exclusion (a directory in its place is
// unreadable even as root, unlike a chmod).
func TestProcessPerfUnreadableResultErrors(t *testing.T) {
	repoDir := newPanelBackend(t)
	seedDelivered(t, repoDir, "F-0001", panelAt(1), "-")
	if err := os.MkdirAll(filepath.Join(repoDir, "content", "features", "F-0001", "result.md"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if _, err := ProcessPerf(repoDir, panelAt(1000)); err == nil {
		t.Fatalf("ProcessPerf over an unreadable result succeeded, want an error")
	}
}

// TestProcessPerfExcludesEpics pins C12: a feature with children is an epic
// whose own anchor holds only its orchestration — sampled, it would read as a
// cheap independent delivery. It is excluded and counted, the children stay.
func TestProcessPerfExcludesEpics(t *testing.T) {
	repoDir := newPanelBackend(t)
	seedDecision(t, repoDir, "D-0001", panelAt(0), domain.DecisionActive, []string{"process"}, []string{"metric"})
	seedDelivered(t, repoDir, "E-0001", panelAt(5), stamped(t, "# E-0001\n", cost.Counters{Input: 200_000}))
	for i, id := range []string{"F-0001", "F-0002"} {
		seedDelivered(t, repoDir, id, panelAt(1+i), stamped(t, "# "+id+"\n", cost.Counters{Input: 9_000_000}))
		f, _ := store.LoadFeature(repoDir, id)
		f.Parent = "E-0001"
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
	}

	r := mustProcessPerf(t, repoDir)
	a := r.Markers[0].After
	if r.ExcludedEpic != 1 || a.ExcludedEpic != 1 || a.N != 2 || a.Delivered != 3 || a.Median != 9_000_000 {
		t.Fatalf("report = excluded_epic %d, after epic %d n %d delivered %d median %v, want 1 / 1 / 2 / 3 / 9M",
			r.ExcludedEpic, a.ExcludedEpic, a.N, a.Delivered, a.Median)
	}
}

// TestProcessPerfReadsUnattributed pins C5 in the collector: the anchor's
// unattributed counters reach the side's coverage.
func TestProcessPerfReadsUnattributed(t *testing.T) {
	repoDir := newPanelBackend(t)
	seedDecision(t, repoDir, "D-0001", panelAt(0), domain.DecisionActive, []string{"process"}, []string{"metric"})
	body, err := cost.Stamp("# F-0001\n", cost.Report{Counters: cost.Counters{Input: 3000}, Unattributed: cost.Counters{Input: 1000}, Messages: 1})
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	seedDelivered(t, repoDir, "F-0001", panelAt(1), body)

	if a := mustProcessPerf(t, repoDir).Markers[0].After; a.UnattributedMedian != 1000 || a.Coverage != 0.75 {
		t.Fatalf("after = unattributed %v coverage %v, want 1000 / 0.75", a.UnattributedMedian, a.Coverage)
	}
}

// TestProcessPerfIgnoresDeadFixers pins C16: a "fix" that was rejected (not a
// bug after all) or superseded is no escaped defect of what it named.
func TestProcessPerfIgnoresDeadFixers(t *testing.T) {
	for _, state := range []domain.FeatureState{domain.StateRejected, domain.StateSuperseded} {
		repoDir := newPanelBackend(t)
		seedDecision(t, repoDir, "D-0001", panelAt(0), domain.DecisionActive, []string{"process"}, []string{"metric"})
		seedDelivered(t, repoDir, "F-0001", panelAt(1), stamped(t, "# F-0001\n", cost.Counters{Input: 1000}))
		fix := store.Feature{ID: "F-0002", Title: "Corrige", Fixes: "F-0001", State: state, CreatedAt: panelAt(6)}
		if err := store.SaveFeature(repoDir, fix); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
		if a := mustProcessPerf(t, repoDir).Markers[0].After; a.EscapedShare == nil || *a.EscapedShare != 0 {
			t.Fatalf("%s fixer: escaped share = %+v, want 0", state, a)
		}
	}
}
