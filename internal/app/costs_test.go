package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0391 crit. 7: a vendor comes only from the models that spent the cost.
func TestCostVendors(t *testing.T) {
	for in, want := range map[string]string{"claude-opus-5-5": "anthropic", "gpt-5": "", "<synthetic>": ""} {
		if got := modelVendor(in); got != want {
			t.Errorf("modelVendor(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCostsCarryTime (F-0528 crit. 12, 14): the costs row and the detail carry
// the active time of the stamped anchor; a stamp from before the time existed
// carries none (nil, never zero).
func TestCostsCarryTime(t *testing.T) { // F-0528 crit. 12, 14
	repo := t.TempDir()
	day := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	seed := func(id string, tm *cost.Timing) {
		f := store.Feature{ID: id, Title: id, State: domain.StateImplemented, CreatedAt: day, UpdatedAt: day}
		if err := store.SaveFeature(repo, f); err != nil {
			t.Fatal(err)
		}
		if err := store.AppendEvent(repo, domain.Event{ID: "c" + id, EntityID: id, Type: "feature.completed", At: day}); err != nil {
			t.Fatal(err)
		}
		r := cost.Report{ID: id, Counters: cost.Counters{Input: 5}, Messages: 1, Sessions: 1, ByModel: map[string]cost.Counters{"claude-opus-5-5": {Input: 5}}, Timing: tm}
		body, err := cost.Stamp("# "+id+"\n\n## Resultado\n\nOk.\n", r)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(repo, "content", "features", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "result.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	seed("F-0001", &cost.Timing{ActiveSeconds: 600, WallSeconds: 600, ByPhase: map[string]int64{"plano": 600}, ByActivity: map[string]int64{"pesquisa": 600}})
	seed("F-0002", nil)
	rep, err := Costs(repo, CostsQuery{Since: day, Until: day}, day)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*cost.Timing{}
	for _, f := range rep.Features {
		got[f.ID] = f.Time
	}
	if got["F-0001"] == nil || got["F-0001"].ActiveSeconds != 600 || got["F-0001"].ByPhase["plano"] != 600 {
		t.Fatalf("F-0001 time = %+v, want the stamped 600 s", got["F-0001"])
	}
	if tm, ok := got["F-0002"]; !ok || tm != nil {
		t.Fatalf("F-0002 time = %+v (present %v), want nil", tm, ok)
	}
	d, err := FeatureCost(repo, "F-0001")
	if err != nil || d.Report.Timing == nil || d.Report.ByActivity["pesquisa"] != 600 {
		t.Fatalf("detail report timing = %+v err %v, want by_activity pesquisa 600", d.Report.Timing, err)
	}
}
