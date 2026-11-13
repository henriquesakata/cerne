package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// costSeed is one fixture feature of the costs screen (F-0391).
type costSeed struct {
	id, parent string
	completed  time.Time
	level      string // the spec's Nível line; "" writes no spec
	agent      string // the result's Agente line; "" writes none
	gauntlet   string // the result's Gauntlet section body
	provider   string // a doc write's provider (F-0375); "" records none
	report     *cost.Report
}

func seedCosts(t *testing.T, repoDir string, seeds []costSeed) {
	t.Helper()
	for _, s := range seeds {
		f := store.Feature{ID: s.id, Title: "Feature " + s.id, Parent: s.parent, State: domain.StateImplemented, CreatedAt: s.completed, UpdatedAt: s.completed}
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
		if err := store.AppendEvent(repoDir, domain.Event{ID: "c" + s.id, EntityID: s.id, Type: "feature.completed", At: s.completed}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
		if s.provider != "" {
			ev := domain.Event{ID: "w" + s.id, EntityID: s.id, Type: "feature.doc_edited", At: s.completed,
				Payload: map[string]string{"doc": "result", "provider": s.provider, "source": "llm-config"}}
			if err := store.AppendEvent(repoDir, ev); err != nil {
				t.Fatalf("AppendEvent: %v", err)
			}
		}
		dir := filepath.Join(repoDir, "content", "features", s.id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if s.level != "" {
			spec := "# " + s.id + "\n\n**Tipo:** Novo recurso\n**Nível:** " + s.level + " — porque sim\n\n## Problema\n"
			if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(spec), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
		}
		body := "# " + s.id + "\n\n"
		if s.agent != "" {
			body += "**Agente:** " + s.agent + "\n\n"
		}
		body += "## Resultado\n\nEntregue.\n"
		if s.gauntlet != "" {
			body += "\n## Gauntlet\n\n" + s.gauntlet + "\n"
		}
		if s.report != nil {
			var err error
			if body, err = cost.Stamp(body, *s.report); err != nil {
				t.Fatalf("Stamp: %v", err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "result.md"), []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
}

func costDay(m time.Month, d int) time.Time { return time.Date(2026, m, d, 12, 0, 0, 0, time.UTC) }

// seedCostsFixture: W40 holds F-0001..F-0003 (weighted 100, 200, 600), W41
// only the unstamped F-0004, W42 F-0006 (1000) and the epic F-0007; F-0005
// sits after the period. D-0001 marks a change inside it, D-0002 after it.
func seedCostsFixture(t *testing.T, repoDir string) {
	t.Helper()
	opus, sonnet := "claude-opus-5-5", "claude-sonnet-5-5"
	seedCosts(t, repoDir, []costSeed{
		{id: "F-0001", completed: costDay(9, 29), level: "leve", agent: opus + " · esforço high",
			gauntlet: "Barra: x\nRodada 1 — derrota: \"a\"\nRodada 2 — vitória: \"b\"",
			report: &cost.Report{ID: "F-0001", Counters: cost.Counters{Input: 50, Output: 10}, Messages: 3, Sessions: 1,
				ByModel: map[string]cost.Counters{opus: {Input: 50, Output: 10}},
				ByRole:  map[string]cost.Counters{"builder": {Input: 50}, "critic": {Output: 6}, "judge": {Output: 4}}}},
		{id: "F-0002", completed: costDay(9, 30), level: "padrão", agent: sonnet + " · esforço desconhecido", provider: "openai",
			gauntlet: "Barra: x\nRodada 1 — vitória: \"a\"",
			report: &cost.Report{ID: "F-0002", Counters: cost.Counters{CacheRead: 2000}, Messages: 2, Sessions: 1,
				ByModel: map[string]cost.Counters{sonnet: {CacheRead: 2000}},
				ByRole:  map[string]cost.Counters{"main": {CacheRead: 1000}, "builder": {CacheRead: 1000}}}},
		{id: "F-0003", completed: costDay(10, 1), level: "avançado",
			report: &cost.Report{ID: "F-0003", Counters: cost.Counters{CacheCreation: 480}, Messages: 1, Sessions: 1,
				ByModel: map[string]cost.Counters{"gpt-5": {CacheCreation: 480}}}},
		{id: "F-0004", completed: costDay(10, 6), level: "leve"},
		{id: "F-0005", completed: costDay(10, 25),
			report: &cost.Report{ID: "F-0005", Counters: cost.Counters{Input: 9}, Messages: 1, Sessions: 1, ByModel: map[string]cost.Counters{opus: {Input: 9}}}},
		{id: "F-0006", parent: "F-0007", completed: costDay(10, 13), agent: opus + " · esforço medium",
			gauntlet: "Barra: dispensada por decisão humana (D-0009)",
			report: &cost.Report{ID: "F-0006", Counters: cost.Counters{Input: 1000}, Messages: 4, Sessions: 1,
				ByModel: map[string]cost.Counters{opus: {Input: 1000}},
				ByRole:  map[string]cost.Counters{"builder": {Input: 700}, "other": {Input: 200}}}},
		{id: "F-0007", completed: costDay(10, 14),
			report: &cost.Report{ID: "F-0007", Counters: cost.Counters{Input: 5}, Messages: 1, Sessions: 1, ByModel: map[string]cost.Counters{opus: {Input: 5}}}},
	})
	for _, d := range []store.Decision{
		{ID: "D-0001", Title: "Modelo novo do dev", CreatedAt: costDay(10, 2)},
		{ID: "D-0002", Title: "Fora do período", CreatedAt: costDay(11, 2)},
	} {
		d.State, d.Facets, d.Components = domain.DecisionActive, []string{"process"}, []string{"metric"}
		if err := store.SaveDecision(repoDir, d); err != nil {
			t.Fatalf("SaveDecision: %v", err)
		}
	}
}

func getCosts(t *testing.T, repoDir, projDir, query string) app.CostsReport {
	t.Helper()
	rec := httptest.NewRecorder()
	New(repoDir, projDir, "proj").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/costs"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/costs%s = %d: %s", query, rec.Code, rec.Body.String())
	}
	var r app.CostsReport
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return r
}

func costIDs(r app.CostsReport) []string {
	var ids []string
	for _, f := range r.Features {
		ids = append(ids, f.ID)
	}
	return ids
}

const costsPeriod = "?since=2026-09-28&until=2026-10-18"

// Crit. 1, 2, 3, 7: the period's stamped features, the weighted mean and
// median, the weekly series, the role composition and the coverage.
func TestCostsAggregates(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedCostsFixture(t, repoDir)
	r := getCosts(t, repoDir, projDir, costsPeriod)

	if got, want := costIDs(r), []string{"F-0001", "F-0002", "F-0003", "F-0006"}; !slices.Equal(got, want) {
		t.Fatalf("features = %v, want %v (no anchor, epic and out-of-period left out)", got, want)
	}
	if r.Since != "2026-09-28" || r.Until != "2026-10-18" {
		t.Errorf("period = %s..%s", r.Since, r.Until)
	}
	if r.Mean != 475 || r.Median != 400 {
		t.Errorf("mean, median = %v, %v; want 475, 400", r.Mean, r.Median)
	}
	if want := map[string]float64{"input": 1, "cache_creation": 1.25, "cache_read": 0.1, "output": 5}; !mapsEqual(r.Weights, want) {
		t.Errorf("weights = %v, want %v", r.Weights, want)
	}
	if len(r.Weeks) != 2 || r.Weeks[0].Week != "2026-W40" || r.Weeks[0].N != 3 || r.Weeks[0].Median != 200 || r.Weeks[0].Mean != 300 ||
		r.Weeks[1].Week != "2026-W42" || r.Weeks[1].N != 1 || r.Weeks[1].Median != 1000 {
		t.Errorf("weeks = %+v, want W40 (n 3, median 200, mean 300) and W42 (n 1, 1000)", r.Weeks)
	}
	if want := map[string]float64{"builder": 850, "critic": 30, "judge": 20, "main": 100, "other": 200, "unknown": 700}; !mapsEqual(r.Roles, want) {
		t.Errorf("roles = %v, want %v", r.Roles, want)
	}
	want := app.CostCoverage{Delivered: 6, Stamped: 4, Epics: 1, Level: 3, Effort: 2, Vendor: 3, Roles: 3, Matched: 4}
	if r.Coverage != want {
		t.Errorf("coverage = %+v, want %+v", r.Coverage, want)
	}
	if len(r.Markers) != 1 || r.Markers[0].ID != "D-0001" || r.Markers[0].Verdict == "" || r.MinSample == 0 {
		t.Errorf("markers = %+v, want D-0001 alone, with its before/after verdict", r.Markers)
	}
}

// Crit. 4, 7: each row carries its level, weighted cost, Gauntlet share,
// rounds, defeats, declared effort and the vendors its data names.
func TestCostsRows(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedCostsFixture(t, repoDir)
	r := getCosts(t, repoDir, projDir, costsPeriod)
	if len(r.Features) != 4 {
		t.Fatalf("features = %v", costIDs(r))
	}
	f1, f2, f3, f6 := r.Features[0], r.Features[1], r.Features[2], r.Features[3]
	if f1.Level != "leve" || f1.Weighted != 100 || f1.Rounds != 2 || f1.Defeats != 1 || !f1.HasGauntlet ||
		f1.GauntletShare == nil || *f1.GauntletShare != 0.5 || f1.Effort != "high" || f1.EffortSource != "declared" ||
		f1.ByRole["critic"].Output != 6 || f1.WeightedByModel["claude-opus-5-5"] != 100 || !slices.Equal(f1.Vendors, []string{"anthropic"}) {
		t.Errorf("F-0001 = %+v", f1)
	}
	if f2.Effort != "" || f2.EffortSource != "unknown" || f2.GauntletShare == nil || *f2.GauntletShare != 0 ||
		!slices.Equal(f2.Vendors, []string{"anthropic", "openai"}) {
		t.Errorf("F-0002 = %+v, want effort unknown, Gauntlet share 0, vendors anthropic+openai", f2)
	}
	if f3.EffortSource != "unknown" || f3.GauntletShare != nil || f3.WeightedByRole["unknown"] != 600 || len(f3.Vendors) != 0 || f3.HasGauntlet {
		t.Errorf("F-0003 = %+v, want no Gauntlet share, all of it unknown role, no vendor, no Gauntlet", f3)
	}
	if f6.Level != "" || f6.WeightedByRole["unknown"] != 100 || f6.HasGauntlet || f6.Rounds != 0 {
		t.Errorf("F-0006 = %+v, want no level and the uncovered 100 as unknown role", f6)
	}
	if !f1.CompletedAt.Equal(costDay(9, 29)) || f1.State != domain.StateImplemented || f1.Title != "Feature F-0001" {
		t.Errorf("F-0001 identity = %s %s %s", f1.Title, f1.State, f1.CompletedAt)
	}
}

// Crit. 7: every filter narrows every view; the options list only values the
// period holds (no vendor without data) and stay put while filtering.
func TestCostsFilters(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedCostsFixture(t, repoDir)
	for _, c := range []struct {
		query string
		want  []string
	}{
		{"&level=leve", []string{"F-0001"}},
		{"&level=unknown", []string{"F-0006"}},
		{"&model=claude-opus-5-5", []string{"F-0001", "F-0006"}},
		{"&vendor=openai", []string{"F-0002"}},
		{"&vendor=anthropic", []string{"F-0001", "F-0002", "F-0006"}},
		{"&effort=high", []string{"F-0001"}},
		{"&effort=unknown", []string{"F-0002", "F-0003"}},
		{"&vendor=anthropic&effort=unknown", []string{"F-0002"}},
	} {
		r := getCosts(t, repoDir, projDir, costsPeriod+c.query)
		if got := costIDs(r); !slices.Equal(got, c.want) {
			t.Errorf("%s: features = %v, want %v", c.query, got, c.want)
		}
		if r.Coverage.Matched != len(c.want) || r.Coverage.Stamped != 4 {
			t.Errorf("%s: coverage = %+v, want matched %d of 4", c.query, r.Coverage, len(c.want))
		}
		n := 0
		for _, w := range r.Weeks {
			n += w.N
		}
		if n != len(c.want) {
			t.Errorf("%s: weeks hold %d features, want %d", c.query, n, len(c.want))
		}
		if len(r.Options.Vendors) != 2 || r.Options.Vendors[0] != (app.CostOption{Value: "anthropic", N: 3}) || r.Options.Vendors[1] != (app.CostOption{Value: "openai", N: 1}) {
			t.Errorf("%s: vendor options = %+v, want anthropic 3, openai 1", c.query, r.Options.Vendors)
		}
	}
	r := getCosts(t, repoDir, projDir, costsPeriod+"&effort=high")
	if r.Mean != 100 || r.Roles["critic"] != 30 || r.Roles["main"] != 0 {
		t.Errorf("effort=high: mean %v, roles %v — the aggregates must follow the filter", r.Mean, r.Roles)
	}
	wantEfforts := []app.CostOption{{Value: "high", N: 1}, {Value: "medium", N: 1}, {Value: "unknown", N: 2}}
	if !slices.Equal(r.Options.Efforts, wantEfforts) {
		t.Errorf("effort options = %+v, want %+v", r.Options.Efforts, wantEfforts)
	}
	wantLevels := []app.CostOption{{Value: "leve", N: 1}, {Value: "padrão", N: 1}, {Value: "avançado", N: 1}, {Value: "unknown", N: 1}}
	if !slices.Equal(r.Options.Levels, wantLevels) {
		t.Errorf("level options = %+v, want %+v", r.Options.Levels, wantLevels)
	}
	wantModels := []app.CostOption{{Value: "claude-opus-5-5", N: 2}, {Value: "claude-sonnet-5-5", N: 1}, {Value: "gpt-5", N: 1}}
	if !slices.Equal(r.Options.Models, wantModels) {
		t.Errorf("model options = %+v, want %+v", r.Options.Models, wantModels)
	}
}

// Crit. 1: with no since/until the period is the last 8 weeks, today included.
func TestCostsDefaultPeriod(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	in := cost.Report{Counters: cost.Counters{Input: 10}, Messages: 1, Sessions: 1, ByModel: map[string]cost.Counters{}}
	seedCosts(t, repoDir, []costSeed{
		{id: "F-0001", completed: now.Add(-time.Hour), report: &in},
		{id: "F-0002", completed: now.AddDate(0, 0, -60), report: &in},
	})
	r := getCosts(t, repoDir, projDir, "")
	today := now.Format(time.DateOnly)
	if r.Until != today || r.Since != now.AddDate(0, 0, -55).Format(time.DateOnly) {
		t.Errorf("default period = %s..%s, want 8 weeks ending %s", r.Since, r.Until, today)
	}
	if got := costIDs(r); !slices.Equal(got, []string{"F-0001"}) {
		t.Errorf("features = %v, want only F-0001", got)
	}
}

// Crit. 8: no stamped feature is a 200 with empty lists, never a made-up number.
func TestCostsEmpty(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedCosts(t, repoDir, []costSeed{{id: "F-0001", completed: costDay(10, 1)}})
	rec := httptest.NewRecorder()
	New(repoDir, projDir, "proj").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/costs"+costsPeriod, nil))
	body := rec.Body.String()
	for _, want := range []string{`"features":\s*\[\]`, `"weeks":\s*\[\]`, `"markers":\s*\[\]`, `"delivered":\s*1,`, `"stamped":\s*0,`, `"mean":\s*0,`} {
		if !regexp.MustCompile(want).MatchString(body) {
			t.Errorf("body lacks %s: %s", want, body)
		}
	}
}

func TestCostsBadPeriod(t *testing.T) {
	repoDir, projDir := newBackend(t)
	for _, q := range []string{"?since=2026-13-01", "?until=ontem", "?since=2026-10-02&until=2026-10-01"} {
		rec := httptest.NewRecorder()
		New(repoDir, projDir, "proj").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/costs"+q, nil))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"error"`) {
			t.Errorf("%s = %d %s, want 400 with a body", q, rec.Code, rec.Body.String())
		}
	}
}

// Crit. 5: the detail is the stamped report (as cost --json reads it) and the
// rounds with their outcomes; an invalid, unknown or unstamped id is a 404
// with a body that says which.
func TestCostDetail(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedCostsFixture(t, repoDir)
	rec := httptest.NewRecorder()
	New(repoDir, projDir, "proj").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/costs/F-0001", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var d app.CostDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.ID != "F-0001" || d.Report.Source != "stamp" || d.Report.Messages != 3 || d.Report.ByRole["judge"].Output != 4 ||
		d.Effort != "high" || d.Weighted != 100 {
		t.Errorf("detail = %+v", d)
	}
	if len(d.RoundList) != 2 || d.RoundList[0].Outcome != "loss" || d.RoundList[1].Outcome != "win" {
		t.Errorf("rounds = %+v, want loss then win", d.RoundList)
	}
	for id, want := range map[string]string{"bogus": "not a feature id", "F-9999": "not found", "F-0004": "cerne cost F-0004 --stamp"} {
		rec := httptest.NewRecorder()
		New(repoDir, projDir, "proj").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/costs/"+id, nil))
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s = %d %s, want 404 naming %q", id, rec.Code, rec.Body.String(), want)
		}
	}
}

func mapsEqual(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range b {
		if got, ok := a[k]; !ok || got != v {
			return false
		}
	}
	return true
}
