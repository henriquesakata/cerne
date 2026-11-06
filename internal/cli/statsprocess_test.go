package cli

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

// seedPanel stages the process panel's inputs straight in the clone (stats is
// read-only): a process+metric marker D-0001 on 2026-03-02, two stamped
// deliveries after it and one delivered without a stamp.
func seedPanel(t *testing.T, repoDir string, stampedToo bool) {
	t.Helper()
	day := func(n int) time.Time { return time.Date(2026, 3, n, 12, 0, 0, 0, time.UTC) }
	d := store.Decision{ID: "D-0001", Title: "Partida enxuta", State: domain.DecisionActive,
		Facets: []string{"process"}, Components: []string{"metric"}, CreatedAt: day(2), UpdatedAt: day(2)}
	if err := store.SaveDecision(repoDir, d); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	deliver := func(id string, at time.Time, result string) {
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Feature " + id, State: domain.StateImplemented, CreatedAt: at}); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
		ev := domain.Event{ID: "ev-" + id, EntityID: id, Type: "feature.completed", At: at}
		if err := store.AppendEvent(repoDir, ev); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
		dir := filepath.Join(repoDir, "content", "features", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "result.md"), []byte(result), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	stamp := func(body string, c cost.Counters) string {
		out, err := cost.Stamp(body, cost.Report{Counters: c, Messages: 1})
		if err != nil {
			t.Fatalf("Stamp: %v", err)
		}
		return out
	}
	deliver("F-0001", day(1), "# F-0001\n\n## Resultado\n\nfeito\n")
	if !stampedToo {
		return
	}
	deliver("F-0002", day(3), stamp("## Gauntlet\n\nBarra: x\nRodada 1 — derrota: \"a\"\nRodada 2 — vitória: \"b\"\n",
		cost.Counters{Input: 302, CacheCreation: 339857, CacheRead: 28934595, Output: 150077}))
	deliver("F-0003", day(4), stamp("## Resultado\n\nfeito\n", cost.Counters{Input: 1000, CacheRead: 20_000_000, Output: 100_000}))
}

// TestStatsProcessText pins criteria 13-14 on the text output: the series, the
// marker with its windows and a verdict carrying its reason, and the exclusions.
func TestStatsProcessText(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedPanel(t, repoDir, true)

	out, err := execCerne(t, "stats", "--process", "--dir", projDir)
	if err != nil {
		t.Fatalf("stats --process: %v\noutput: %s", err, out)
	}
	for _, want := range []string{
		"Process performance",
		"only sessions bound to the feature count",
		"2026-W10",
		"D-0001",
		"before",
		"after",
		"verdict: inconclusivo — small sample (n=0 before, n=2 after",
		"Excluded: 1 delivered without a stamped cost · 1 without a Gauntlet record",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stats --process missing %q:\n%s", want, out)
		}
	}
}

// TestStatsProcessJSONGolden pins criterion 14's stable JSON: fixed dates and
// n < 8, so nothing depends on now or the bootstrap.
func TestStatsProcessJSONGolden(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedPanel(t, repoDir, true)

	out, err := execCerne(t, "stats", "--process", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("stats --process --json: %v\noutput: %s", err, out)
	}
	if out != processGolden {
		t.Fatalf("stats --process --json =\n%s\nwant\n%s", out, processGolden)
	}
}

const processGolden = `{
  "weeks": [
    {
      "week": "2026-W10",
      "start": "2026-03-02T00:00:00Z",
      "median": 3284983.875,
      "n": 2
    }
  ],
  "markers": [
    {
      "id": "D-0001",
      "title": "Partida enxuta",
      "at": "2026-03-02T12:00:00Z",
      "before": {
        "from": "2026-02-02T12:00:00Z",
        "to": "2026-03-02T12:00:00Z",
        "n": 0,
        "median": 0,
        "p25": 0,
        "p75": 0,
        "raw_median": {
          "input": 0,
          "cache_creation": 0,
          "cache_read": 0,
          "output": 0
        },
        "with_gauntlet": 0,
        "returned_share": 0,
        "escaped_share": 0
      },
      "after": {
        "from": "2026-03-02T12:00:00Z",
        "to": "2026-03-30T12:00:00Z",
        "n": 2,
        "median": 3284983.875,
        "p25": 2501000,
        "p75": 4068967.75,
        "raw_median": {
          "input": 651,
          "cache_creation": 169928.5,
          "cache_read": 24467297.5,
          "output": 125038.5
        },
        "with_gauntlet": 1,
        "returned_share": 1,
        "escaped_share": 0
      },
      "verdict": "inconclusivo",
      "code": "small_sample",
      "reason": "small sample (n=0 before, n=2 after; 8 per side needed)",
      "guarded": false
    }
  ],
  "excluded_no_cost": 1,
  "excluded_no_gauntlet": 1
}
`

// TestStatsProcessNoAnchorsReason pins criterion 16 at the CLI: no stamped
// delivery prints the explicit reason, not an empty series nor an error.
func TestStatsProcessNoAnchorsReason(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedPanel(t, repoDir, false)

	out, err := execCerne(t, "stats", "--process", "--dir", projDir)
	if err != nil {
		t.Fatalf("stats --process: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "no feature with a stamped cost — depends on F-0309") || strings.Contains(out, "Weekly median") {
		t.Fatalf("stats --process without anchors =\n%s\nwant the F-0309 reason and no weeks line", out)
	}
}

// TestStatsProcessRefusesSince: the panel's windows are relative to each
// marker, so a --since it would silently ignore is refused.
func TestStatsProcessRefusesSince(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "stats", "--process", "--since", "90d", "--dir", projDir); err == nil {
		t.Fatalf("stats --process --since succeeded, want the exclusive-flags error")
	}
}

// TestRenderProcessQualifiesUnguarded pins C9 in the text: an unguarded
// melhorou names the guards it still lacks next to the verdict.
func TestRenderProcessQualifiesUnguarded(t *testing.T) {
	var out strings.Builder
	renderProcess(&out, stats.ProcessReport{Weeks: []stats.WeekCost{}, Markers: []stats.MarkerReport{{
		Marker: stats.Marker{ID: "D-0001"}, Verdict: "melhorou", Code: "cost", Reason: "median ratio 0.60 (90% CI 0.39–0.91), cost down 40%",
		Cost: "melhorou", Returned: "inconclusivo", Escaped: "pending",
	}}})
	if !strings.Contains(out.String(), "verdict: melhorou (unguarded: returned inconclusive, escaped pending) — median ratio") {
		t.Fatalf("render =\n%s\nwant the unguarded qualifier next to melhorou", out.String())
	}
}
