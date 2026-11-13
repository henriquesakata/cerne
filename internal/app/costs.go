package app

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/restmpl"
	"github.com/henriquesakata/cerne/internal/stats"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrNoCostStamped: the feature exists but its result carries no cost anchor —
// the costs screen shows only measured data (F-0391).
var ErrNoCostStamped = errors.New("no cost stamped")

// CostsQuery is the costs screen's period — [Since, Until], whole UTC days —
// and its filters; "" keeps every value, "unknown" picks the features with no
// level or no declared effort.
type CostsQuery struct {
	Since, Until                 time.Time
	Level, Model, Vendor, Effort string
}

// CostFeature is one delivered feature with a stamped cost (F-0391).
type CostFeature struct {
	ID              string                   `json:"id"`
	Title           string                   `json:"title"`
	Level           string                   `json:"level"` // leve | padrão | avançado; "" with no level line
	State           domain.FeatureState      `json:"state"`
	CompletedAt     time.Time                `json:"completed_at"` // the first feature.completed
	Weighted        float64                  `json:"weighted"`
	Tokens          cost.Counters            `json:"tokens"`
	ByRole          map[string]cost.Counters `json:"by_role"`
	ByModel         map[string]cost.Counters `json:"by_model"`
	WeightedByRole  map[string]float64       `json:"weighted_by_role"`  // no role breakdown: all of it "unknown"
	WeightedByModel map[string]float64       `json:"weighted_by_model"` // no model breakdown: all of it "unknown"
	GauntletShare   *float64                 `json:"gauntlet_share"`    // critic + judge over the whole; null with no role breakdown
	HasGauntlet     bool                     `json:"has_gauntlet"`
	Rounds          int                      `json:"rounds"`
	Defeats         int                      `json:"defeats"`
	Effort          string                   `json:"effort"`
	EffortSource    string                   `json:"effort_source"` // declared | unknown
	Vendors         []string                 `json:"vendors"`
}

// CostWeek is one ISO week (UTC) of the filtered features.
type CostWeek struct {
	Week   string    `json:"week"`  // "2026-W45"
	Start  time.Time `json:"start"` // Monday 00:00 UTC
	Median float64   `json:"median"`
	Mean   float64   `json:"mean"`
	N      int       `json:"n"`
}

// CostCoverage says how much of the period the numbers see: the deliveries,
// those with a stamped cost (epics out, counted), and — over the stamped —
// how many carry each filter's datum.
type CostCoverage struct {
	Delivered int `json:"delivered"`
	Stamped   int `json:"stamped"`
	Epics     int `json:"epics"`
	Level     int `json:"level"`
	Model     int `json:"model"`
	Effort    int `json:"effort"`
	Vendor    int `json:"vendor"`
	Roles     int `json:"roles"`
	Matched   int `json:"matched"` // after the filters
}

// CostOption is a filter value present in the period, with its count.
type CostOption struct {
	Value string `json:"value"`
	N     int    `json:"n"`
}

// CostOptions are the values each filter offers: only what the period's
// stamped features hold, so a vendor with no data is never offered.
type CostOptions struct {
	Levels  []CostOption `json:"levels"`
	Models  []CostOption `json:"models"`
	Vendors []CostOption `json:"vendors"`
	Efforts []CostOption `json:"efforts"`
}

// CostsReport is the costs screen (F-0391).
type CostsReport struct {
	Since      string               `json:"since"`
	Until      string               `json:"until"`
	Weights    map[string]float64   `json:"weights"`
	Mean       float64              `json:"mean"`
	Median     float64              `json:"median"`
	Features   []CostFeature        `json:"features"`
	Weeks      []CostWeek           `json:"weeks"`
	Roles      map[string]float64   `json:"roles"`
	Markers    []stats.MarkerReport `json:"markers"`
	MinSample  int                  `json:"min_sample"`
	GuardLevel float64              `json:"guard_level"`
	Coverage   CostCoverage         `json:"coverage"`
	Options    CostOptions          `json:"options"`
}

// CostDetail is one feature's cost: the row, the stamped report and the rounds.
type CostDetail struct {
	CostFeature
	Report    cost.Report         `json:"report"`
	RoundList []lint.RoundOutcome `json:"round_list"`
	// WriteProviders: who drafted the feature's documents (F-0375) — not who
	// spent the tokens, so never a vendor of the cost.
	WriteProviders []string `json:"write_providers"`
}

// Costs builds the costs screen (F-0391) from the backend clone at repoDir:
// the features first completed in the period whose result carries a non-zero
// cost anchor (an epic's own anchor is orchestration only: out, counted — the
// process panel's rules), narrowed by the filters, with their weekly series,
// role composition and the process panel's markers inside the period. The
// options and the coverage are the period's, before the filters. Read-only:
// the anchors and the specs, never a transcript.
func Costs(repoDir string, q CostsQuery, now time.Time) (CostsReport, error) {
	from, to := q.Since, q.Until.AddDate(0, 0, 1)
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		return CostsReport{}, err
	}
	feats, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		return CostsReport{}, err
	}
	byID, epic := map[string]store.Feature{}, map[string]bool{}
	for _, f := range feats {
		byID[f.ID] = f
		if f.Parent != "" {
			epic[f.Parent] = true
		}
	}
	rep := CostsReport{Since: q.Since.Format(time.DateOnly), Until: q.Until.Format(time.DateOnly), Weights: hubWeights(),
		Features: []CostFeature{}, Weeks: []CostWeek{}, Roles: map[string]float64{}, Markers: []stats.MarkerReport{}}
	ids, completed := completions(events)
	var all []CostFeature
	for _, id := range ids {
		at := completed[id]
		f, known := byID[id]
		if at.Before(from) || !at.Before(to) || !known {
			continue
		}
		rep.Coverage.Delivered++
		if epic[id] {
			rep.Coverage.Epics++
			continue
		}
		d, ok, err := readCost(repoDir, f, at)
		if err != nil {
			return CostsReport{}, err
		}
		if ok {
			all = append(all, d.CostFeature)
		}
	}
	slices.SortFunc(all, func(a, b CostFeature) int {
		if c := a.CompletedAt.Compare(b.CompletedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	rep.Coverage.Stamped, rep.Options = len(all), costOptions(all)
	for _, f := range all {
		rep.Coverage.Level += count(f.Level != "")
		rep.Coverage.Model += count(len(f.ByModel) > 0)
		rep.Coverage.Effort += count(f.EffortSource == "declared")
		rep.Coverage.Vendor += count(len(f.Vendors) > 0)
		rep.Coverage.Roles += count(len(f.ByRole) > 0)
		if q.matches(f) {
			rep.Features = append(rep.Features, f)
		}
	}
	rep.Coverage.Matched = len(rep.Features)
	var costs []float64
	for _, f := range rep.Features {
		costs = append(costs, f.Weighted)
		for role, v := range f.WeightedByRole {
			rep.Roles[role] += v
		}
	}
	rep.Mean, rep.Median = mean(costs), stats.Median(costs)
	rep.Weeks = costWeeks(rep.Features)
	process, err := ProcessPerf(repoDir, now)
	if err != nil {
		return CostsReport{}, err
	}
	rep.MinSample, rep.GuardLevel = process.MinSample, process.GuardLevel
	for _, m := range process.Markers {
		if !m.At.Before(from) && m.At.Before(to) {
			rep.Markers = append(rep.Markers, m)
		}
	}
	return rep, nil
}

// FeatureCost is the costs detail of feature id: its row, the stamped report
// (`cerne cost <id> --json` read from the stamp), its Gauntlet rounds and the
// providers its document writes recorded.
// store.ErrNotFound for an unknown id; ErrNoCostStamped with no non-zero anchor.
func FeatureCost(repoDir, id string) (CostDetail, error) {
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		return CostDetail{}, err
	}
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		return CostDetail{}, err
	}
	_, completed := completions(events)
	d, ok, err := readCost(repoDir, f, completed[id])
	if err != nil {
		return CostDetail{}, err
	}
	if !ok {
		return CostDetail{}, fmt.Errorf("%w for %s: run 'cerne cost %s --stamp' once it is implemented", ErrNoCostStamped, id, id)
	}
	d.WriteProviders = writeProviders(events, id)
	return d, nil
}

// readCost reads feature f's stamped cost and what the screen shows beside it:
// the spec's level, the declared effort, the Gauntlet record and the vendors
// its models name.
// false with no anchor or an all-zero one (the process panel's no_cost).
func readCost(repoDir string, f store.Feature, at time.Time) (CostDetail, bool, error) {
	body, err := os.ReadFile(FeatureDocPath(repoDir, f.ID, "result.md"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return CostDetail{}, false, fmt.Errorf("costs: read the result of %s: %w", f.ID, err)
	}
	r, ok := cost.StampedReport(string(body))
	if !ok || r.Counters == (cost.Counters{}) {
		return CostDetail{}, false, nil
	}
	spec, err := os.ReadFile(FeatureDocPath(repoDir, f.ID, "spec.md"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return CostDetail{}, false, fmt.Errorf("costs: read the spec of %s: %w", f.ID, err)
	}
	level, _ := doccheck.SpecLevel(spec)
	has, defeats := lint.GauntletRounds(body)
	d := CostDetail{Report: r, RoundList: lint.GauntletRoundList(body)}
	d.CostFeature = CostFeature{ID: f.ID, Title: f.Title, Level: level, State: f.State, CompletedAt: at,
		Weighted: weigh(r.Counters), Tokens: r.Counters, ByRole: r.ByRole, ByModel: r.ByModel,
		WeightedByRole: map[string]float64{}, WeightedByModel: map[string]float64{},
		HasGauntlet: has, Rounds: len(d.RoundList), Defeats: defeats, Effort: restmpl.DeclaredEffort(body), EffortSource: "declared"}
	if d.Effort == "" {
		d.EffortSource = "unknown"
	}
	for role, c := range r.ByRole {
		d.WeightedByRole[role] = weigh(c)
	}
	if len(r.ByRole) == 0 {
		d.WeightedByRole["unknown"] = d.Weighted
	} else {
		share := (d.WeightedByRole["critic"] + d.WeightedByRole["judge"]) / d.Weighted
		d.GauntletShare = &share
	}
	// The vendor is the cost's: only the models that spent these tokens.
	vendors := map[string]bool{}
	for m, c := range r.ByModel {
		d.WeightedByModel[m] = weigh(c)
		vendors[modelVendor(m)] = true
	}
	delete(vendors, "")
	if len(r.ByModel) == 0 {
		d.WeightedByModel["unknown"] = d.Weighted
	}
	d.Vendors = slices.Sorted(maps.Keys(vendors))
	return d, true, nil
}

// matches reports whether f passes q's filters.
func (q CostsQuery) matches(f CostFeature) bool {
	pick := func(want, have string) bool {
		return want == "" || want == have || want == "unknown" && have == ""
	}
	_, model := f.ByModel[q.Model]
	return pick(q.Level, f.Level) && pick(q.Effort, f.Effort) &&
		(q.Model == "" || model) && (q.Vendor == "" || slices.Contains(f.Vendors, q.Vendor))
}

// costOptions lists each filter's values among fs, with their counts: levels
// in the scale's order, the rest by name, "unknown" last.
func costOptions(fs []CostFeature) CostOptions {
	levels, models, vendors, efforts := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	for _, f := range fs {
		levels[cmpOr(f.Level, "unknown")]++
		efforts[cmpOr(f.Effort, "unknown")]++
		for m := range f.ByModel {
			models[m]++
		}
		for _, v := range f.Vendors {
			vendors[v]++
		}
	}
	list := func(n map[string]int, order []string) []CostOption {
		keys := slices.Sorted(maps.Keys(n))
		if order != nil {
			keys = slices.DeleteFunc(slices.Clone(order), func(k string) bool { return n[k] == 0 })
		} else if n["unknown"] > 0 {
			keys = append(slices.DeleteFunc(keys, func(k string) bool { return k == "unknown" }), "unknown")
		}
		out := []CostOption{}
		for _, k := range keys {
			out = append(out, CostOption{Value: k, N: n[k]})
		}
		return out
	}
	return CostOptions{Levels: list(levels, []string{"leve", "padrão", "avançado", "unknown"}), Models: list(models, nil),
		Vendors: list(vendors, nil), Efforts: list(efforts, nil)}
}

// costWeeks groups fs (sorted by completion) by ISO week (UTC).
func costWeeks(fs []CostFeature) []CostWeek {
	out := []CostWeek{}
	var costs [][]float64
	for _, f := range fs {
		t := f.CompletedAt.UTC()
		y, w := t.ISOWeek()
		if key := fmt.Sprintf("%04d-W%02d", y, w); len(out) == 0 || out[len(out)-1].Week != key {
			day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			out = append(out, CostWeek{Week: key, Start: day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))})
			costs = append(costs, nil)
		}
		costs[len(costs)-1] = append(costs[len(costs)-1], f.Weighted)
	}
	for i, c := range costs {
		out[i].Median, out[i].Mean, out[i].N = stats.Median(c), mean(c), len(c)
	}
	return out
}

// completions is each entity's first feature.completed, in the order the
// entities first completed.
func completions(events []domain.Event) ([]string, map[string]time.Time) {
	completed := map[string]time.Time{}
	var ids []string
	for _, ev := range events {
		if ev.Type != "feature.completed" {
			continue
		}
		if t, ok := completed[ev.EntityID]; !ok {
			ids = append(ids, ev.EntityID)
			completed[ev.EntityID] = ev.At
		} else if ev.At.Before(t) {
			completed[ev.EntityID] = ev.At
		}
	}
	return ids, completed
}

// writeProviders is the distinct providers feature id's document writes
// recorded (F-0375), sorted: who drafted, never who spent the cost.
func writeProviders(events []domain.Event, id string) []string {
	seen := map[string]bool{}
	for _, ev := range events {
		if p := strings.TrimSpace(ev.Payload["provider"]); ev.EntityID == id && ev.Type == "feature.doc_edited" && p != "" {
			seen[p] = true
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// modelVendor is the vendor a model id names: claude-* is Anthropic's; any
// other id names none — never guessed.
func modelVendor(model string) string {
	if strings.HasPrefix(model, "claude-") {
		return "anthropic"
	}
	return ""
}

// hubWeights names the weights stats.Weighted applies, read from it — one
// unit of each counter — so the screen never restates them.
func hubWeights() map[string]float64 {
	return map[string]float64{"input": stats.Weighted([4]int64{1, 0, 0, 0}), "cache_creation": stats.Weighted([4]int64{0, 1, 0, 0}),
		"cache_read": stats.Weighted([4]int64{0, 0, 1, 0}), "output": stats.Weighted([4]int64{0, 0, 0, 1})}
}

func weigh(c cost.Counters) float64 {
	return stats.Weighted([4]int64{c.Input, c.CacheCreation, c.CacheRead, c.Output})
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func count(b bool) int {
	if b {
		return 1
	}
	return 0
}

func cmpOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
