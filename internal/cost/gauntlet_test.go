package cost

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
)

// stampedFeature saves id in state with a spec at level (none when "") and a
// result stamped with total as its counters and roles as its role breakdown.
func stampedFeature(t *testing.T, repo, id string, state domain.FeatureState, level string, total Counters, roles map[string]Counters) {
	t.Helper()
	saveFeature(t, repo, id, state)
	r := stampReport(id, total, 10)
	r.ByRole = roles
	writeResult(t, repo, id, mustStamp(t, resultDoc(id), r))
	if level != "" {
		spec := "---\ncerne_id: " + id + "\n---\n\n# t\n\n**Nível:** " + level + " — porquê\n"
		if err := os.WriteFile(filepath.Join(repo, "content", "features", id, "spec.md"), []byte(spec), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestGauntletCostStats (F-0390 crit. 1): the critic + judge share of every
// implemented or done feature stamped with those roles, in weighted tokens
// (input 1, cache_creation 1.25, cache_read 0.1, output 5) — the median
// overall and per level, each enough from 3 features.
func TestGauntletCostStats(t *testing.T) {
	repo := t.TempDir()
	in := func(n int64) Counters { return cc(n, 0, 0, 0) }
	// Gauntlet 100 (critic input) + 100 (judge output 20 × 5) = 200 of 1000.
	stampedFeature(t, repo, "F-0001", domain.StateImplemented, "leve", in(1000),
		map[string]Counters{"builder": in(800), "critic": in(100), "judge": cc(0, 0, 0, 20)})
	// Critic cache_creation 80 × 1.25 = 100 of 1000.
	stampedFeature(t, repo, "F-0002", domain.StateDone, "leve", in(1000),
		map[string]Counters{"critic": cc(0, 80, 0, 0)})
	// Judge cache_read 3000 × 0.1 = 300 of 2000.
	stampedFeature(t, repo, "F-0003", domain.StateImplemented, "padrão", in(2000),
		map[string]Counters{"judge": cc(0, 0, 3000, 0)})
	// 300 of 1000.
	stampedFeature(t, repo, "F-0004", domain.StateDone, "leve", in(1000),
		map[string]Counters{"critic": in(300)})
	// No level line: counted overall only. 400 of 1000.
	stampedFeature(t, repo, "F-0008", domain.StateImplemented, "", in(1000),
		map[string]Counters{"critic": in(400)})
	// Out: no critic nor judge; not delivered; delivered with no anchor.
	stampedFeature(t, repo, "F-0005", domain.StateImplemented, "leve", in(1000), map[string]Counters{"builder": in(1000)})
	stampedFeature(t, repo, "F-0006", domain.StateInDevelopment, "leve", in(1000), map[string]Counters{"critic": in(900)})
	saveFeature(t, repo, "F-0007", domain.StateImplemented)
	// Out: an anchor that does not parse; an anchor stamped before F-0384, with no by_role.
	saveFeature(t, repo, "F-0009", domain.StateImplemented)
	writeResult(t, repo, "F-0009", resultDoc("F-0009")+"\n## Custo\n\n<!-- cerne:cost v1 {not json} -->\n")
	stampedFeature(t, repo, "F-0010", domain.StateDone, "leve", in(1000), nil)

	got, err := GauntletCostStats(repo)
	if err != nil {
		t.Fatal(err)
	}
	want := GauntletSample{Features: 5, Enough: true, MedianTokens: 300, MedianShare: 0.2}
	if got.GauntletSample != want {
		t.Errorf("overall = %+v, want %+v", got.GauntletSample, want)
	}
	if l, want := got.ByLevel["leve"], (GauntletSample{Features: 3, Enough: true, MedianTokens: 200, MedianShare: 0.2}); l != want {
		t.Errorf("leve = %+v, want %+v", l, want)
	}
	if p := got.ByLevel["padrão"]; p.Enough || p.Features != 1 || p.MedianTokens != 0 || p.MedianShare != 0 {
		t.Errorf("padrão = %+v, want 1 feature, not enough, no number", p)
	}
}

// TestGauntletCostStatsNotEnough (F-0390 crit. 1): with fewer than 3 measured
// features no number is invented.
func TestGauntletCostStatsNotEnough(t *testing.T) {
	repo := t.TempDir()
	stampedFeature(t, repo, "F-0001", domain.StateImplemented, "leve", cc(1000, 0, 0, 0), map[string]Counters{"critic": cc(100, 0, 0, 0)})
	stampedFeature(t, repo, "F-0002", domain.StateDone, "leve", cc(1000, 0, 0, 0), map[string]Counters{"judge": cc(100, 0, 0, 0)})
	got, err := GauntletCostStats(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enough || got.Features != 2 || got.MedianTokens != 0 || got.MedianShare != 0 {
		t.Errorf("overall = %+v, want 2 features, not enough, no number", got.GauntletSample)
	}
	if l := got.ByLevel["leve"]; l.Enough || l.MedianTokens != 0 {
		t.Errorf("leve = %+v, want not enough", l)
	}
	empty, err := GauntletCostStats(t.TempDir())
	if err != nil || empty.Enough || empty.Features != 0 {
		t.Errorf("empty backend = %+v, %v; want zero, not enough", empty, err)
	}
}
