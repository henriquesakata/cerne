package stats

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
)

// The process panel's rule (F-0321): constants, not config, until a real
// project asks to calibrate them.
const (
	minSample = 8                   // deliveries per side before any verdict
	window    = 28 * 24 * time.Hour // each side of a marker, and the escaped-defect horizon
	resamples = 2000                // bootstrap draws per interval
	// Weights relative to one input token — the hub's custo-agentes.mjs, every
	// cache write at the 5-minute rate: the F-0309 anchor does not split it.
	weightInput, weightCacheCreation, weightCacheRead, weightOutput = 1.0, 1.25, 0.1, 5.0
)

// Verdict and guard literals, as the approved spec writes them.
const (
	improved     = "melhorou"
	worsened     = "piorou"
	inconclusive = "inconclusivo"
	guardOK      = "ok"
	guardPending = "pending"
)

// ProcessSample is one delivered feature with a stamped cost anchor.
type ProcessSample struct {
	ID          string
	CompletedAt time.Time   // first feature.completed
	Raw         [4]int64    // input, cache_creation, cache_read, output — the F-0309 anchor
	HasGauntlet bool        // false: out of the returns guard, never zero returns (crit. 5)
	Returned    bool        // at least one "Rodada N — derrota"
	Fixes       []time.Time // created_at of each feature whose fixes names ID (crit. 19)
}

// Marker is a process change: a decision with facet process + component metric.
type Marker struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}

// Raw holds one side's median of each raw anchor counter.
type Raw struct {
	Input         float64 `json:"input"`
	CacheCreation float64 `json:"cache_creation"`
	CacheRead     float64 `json:"cache_read"`
	Output        float64 `json:"output"`
}

// Side is one window around a marker: [From, To), cut short at CutBy.
type Side struct {
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	CutBy         string    `json:"cut_by,omitempty"`
	N             int       `json:"n"`
	Median        float64   `json:"median"`
	P25           float64   `json:"p25"`
	P75           float64   `json:"p75"`
	RawMedian     Raw       `json:"raw_median"`
	WithGauntlet  int       `json:"with_gauntlet"`
	ReturnedShare float64   `json:"returned_share"` // of WithGauntlet
	EscapedShare  float64   `json:"escaped_share"`  // of N
}

// MarkerReport is a marker judged before/after.
type MarkerReport struct {
	Marker
	Before         Side      `json:"before"`
	After          Side      `json:"after"`
	Verdict        string    `json:"verdict"` // melhorou | piorou | inconclusivo
	Code           string    `json:"code"`    // small_sample | within_noise | cost | quality_guard
	Reason         string    `json:"reason"`
	Cost           string    `json:"cost,omitempty"`     // n >= 8 only
	Returned       string    `json:"returned,omitempty"` // ok | piorou | inconclusivo
	Escaped        string    `json:"escaped,omitempty"`  // ok | piorou | pending
	CostRatioCI    []float64 `json:"cost_ratio_ci,omitempty"`
	ReturnedDiffCI []float64 `json:"returned_diff_ci,omitempty"`
	EscapedDiffCI  []float64 `json:"escaped_diff_ci,omitempty"`
}

// WeekCost is one ISO week (UTC) of the cost series.
type WeekCost struct {
	Week   string    `json:"week"`  // "2026-W45"
	Start  time.Time `json:"start"` // Monday 00:00 UTC
	Median float64   `json:"median"`
	N      int       `json:"n"`
}

// ProcessReport is the process panel (F-0321).
type ProcessReport struct {
	Reason             string         `json:"reason,omitempty"` // no sample at all (crit. 16)
	Weeks              []WeekCost     `json:"weeks"`
	Markers            []MarkerReport `json:"markers"`
	ExcludedNoCost     int            `json:"excluded_no_cost"`
	ExcludedNoGauntlet int            `json:"excluded_no_gauntlet"` // samples out of the returns guard
}

// ComputeProcess builds the process panel from stamped deliveries and change
// markers: the weekly median cost, and per marker the before/after windows
// (28 days, cut at the neighbouring marker) with one verdict. now decides
// whether the escaped-defect guard is still pending (crit. 20).
func ComputeProcess(samples []ProcessSample, markers []Marker, excludedNoCost int, now time.Time) ProcessReport {
	ss := slices.Clone(samples)
	slices.SortFunc(ss, func(a, b ProcessSample) int {
		if c := a.CompletedAt.Compare(b.CompletedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	ms := slices.Clone(markers)
	slices.SortFunc(ms, func(a, b Marker) int {
		if c := a.At.Compare(b.At); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	r := ProcessReport{Weeks: weeks(ss), Markers: make([]MarkerReport, 0, len(ms)), ExcludedNoCost: excludedNoCost}
	if len(ss) == 0 {
		r.Reason = "no feature with a stamped cost — depends on F-0309 (cerne cost <id> --stamp)"
	}
	for _, s := range ss {
		if !s.HasGauntlet {
			r.ExcludedNoGauntlet++
		}
	}
	for i, m := range ms {
		from, cutBefore := m.At.Add(-window), ""
		if i > 0 && ms[i-1].At.After(from) {
			from, cutBefore = ms[i-1].At, ms[i-1].ID
		}
		to, cutAfter := m.At.Add(window), ""
		if i+1 < len(ms) && ms[i+1].At.Before(to) {
			to, cutAfter = ms[i+1].At, ms[i+1].ID
		}
		before, after := between(ss, from, m.At), between(ss, m.At, to)
		mr := MarkerReport{Marker: m, Before: side(before, from, m.At, cutBefore), After: side(after, m.At, to, cutAfter)}
		judge(&mr, before, after, now)
		r.Markers = append(r.Markers, mr)
	}
	return r
}

// judge is the one verdict rule (crit. 9-12, 19, 20), in this order: small
// sample; cost interval; returns guard; escaped guard (pending while young);
// any worse guard vetoes; else noise; else the cost verdict.
func judge(m *MarkerReport, before, after []ProcessSample, now time.Time) {
	b, a := m.Before, m.After
	if b.N < minSample || a.N < minSample {
		m.Verdict, m.Code = inconclusive, "small_sample"
		m.Reason = fmt.Sprintf("small sample (n=%d before, n=%d after; %d per side needed)", b.N, a.N, minSample)
		return
	}
	m.CostRatioCI = bootstrapCI(costs(before), costs(after), func(b, a []float64) float64 { return median(a) / median(b) })
	ratio := a.Median / b.Median
	costText := fmt.Sprintf("median ratio %.2f (90%% CI %.2f–%.2f)", ratio, m.CostRatioCI[0], m.CostRatioCI[1])
	switch {
	case m.CostRatioCI[1] < 1:
		m.Cost = improved
	case m.CostRatioCI[0] > 1:
		m.Cost = worsened
	default:
		m.Cost = inconclusive
	}

	var notes, vetoes []string
	guard := func(label string, b, a []float64) (string, []float64) {
		ci := bootstrapCI(b, a, func(b, a []float64) float64 { return mean(a) - mean(b) })
		text := fmt.Sprintf("%s %+.0f pp (90%% CI %+.0f to %+.0f)", label, (mean(a)-mean(b))*100, ci[0]*100, ci[1]*100)
		if ci[0] > 0 {
			vetoes = append(vetoes, text)
			return worsened, ci
		}
		notes = append(notes, text)
		return guardOK, ci
	}
	if rb, ra := returns(before), returns(after); len(rb) < minSample || len(ra) < minSample {
		m.Returned = inconclusive
		notes = append(notes, fmt.Sprintf("returned guard inconclusive (n=%d before, n=%d after with a Gauntlet)", len(rb), len(ra)))
	} else {
		m.Returned, m.ReturnedDiffCI = guard("returned share", rb, ra)
	}
	if ripe := after[len(after)-1].CompletedAt.Add(window); now.Before(ripe) {
		m.Escaped = guardPending
		notes = append(notes, "escaped defects pending until "+ripe.UTC().Format("2006-01-02"))
	} else {
		m.Escaped, m.EscapedDiffCI = guard("escaped share", escapes(before), escapes(after))
	}

	switch {
	case len(vetoes) > 0:
		link := ", with "
		if m.Cost == improved {
			link = " vetoes "
		}
		m.Verdict, m.Code = worsened, "quality_guard"
		m.Reason = "quality guard: " + strings.Join(vetoes, "; ") + link + costText
	case m.Cost == inconclusive:
		m.Verdict, m.Code = inconclusive, "within_noise"
		m.Reason = "difference within noise: " + costText + joinNotes(notes)
	default:
		dir := "down"
		if ratio > 1 {
			dir = "up"
		}
		m.Verdict, m.Code = m.Cost, "cost"
		m.Reason = fmt.Sprintf("%s, cost %s %.0f%%", costText, dir, math.Abs(ratio-1)*100) + joinNotes(notes)
	}
}

func joinNotes(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	return "; " + strings.Join(notes, "; ")
}

// bootstrapCI is the 90% percentile interval of stat over resamples of both
// sides, each with replacement, from a fresh fixed-seed generator: the same
// input always gives the same interval, whatever else the report holds.
// ponytail: percentile bootstrap, 2000 draws per CI; BCa if a real project shows skewed intervals.
func bootstrapCI(before, after []float64, stat func(b, a []float64) float64) []float64 {
	rng := rand.New(rand.NewPCG(0x0321, 0x2026))
	b, a := make([]float64, len(before)), make([]float64, len(after))
	vals := make([]float64, resamples)
	for i := range vals {
		for j := range b {
			b[j] = before[rng.IntN(len(before))]
		}
		for j := range a {
			a[j] = after[rng.IntN(len(after))]
		}
		vals[i] = stat(b, a)
	}
	slices.Sort(vals)
	return []float64{vals[resamples*5/100-1], vals[resamples*95/100-1]}
}

func side(ss []ProcessSample, from, to time.Time, cutBy string) Side {
	s := Side{From: from, To: to, CutBy: cutBy, N: len(ss)}
	if len(ss) == 0 {
		return s
	}
	c := costs(ss)
	s.Median, s.P25, s.P75 = median(c), quantile(c, 0.25), quantile(c, 0.75)
	var raw [4][]float64
	for _, x := range ss {
		for k := range raw {
			raw[k] = append(raw[k], float64(x.Raw[k]))
		}
	}
	s.RawMedian = Raw{median(raw[0]), median(raw[1]), median(raw[2]), median(raw[3])}
	r := returns(ss)
	s.WithGauntlet, s.ReturnedShare, s.EscapedShare = len(r), mean(r), mean(escapes(ss))
	return s
}

func weeks(ss []ProcessSample) []WeekCost {
	out := []WeekCost{}
	var byWeek [][]float64
	for _, s := range ss { // sorted by time: one week's deliveries are contiguous
		t := s.CompletedAt.UTC()
		y, w := t.ISOWeek()
		key := fmt.Sprintf("%04d-W%02d", y, w)
		if len(out) == 0 || out[len(out)-1].Week != key {
			day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			out = append(out, WeekCost{Week: key, Start: day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))})
			byWeek = append(byWeek, nil)
		}
		byWeek[len(byWeek)-1] = append(byWeek[len(byWeek)-1], weighted(s.Raw))
	}
	for i, c := range byWeek {
		out[i].Median, out[i].N = median(c), len(c)
	}
	return out
}

// between keeps the deliveries completed in [from, to).
func between(ss []ProcessSample, from, to time.Time) []ProcessSample {
	var out []ProcessSample
	for _, s := range ss {
		if !s.CompletedAt.Before(from) && s.CompletedAt.Before(to) {
			out = append(out, s)
		}
	}
	return out
}

func weighted(r [4]int64) float64 {
	return float64(r[0])*weightInput + float64(r[1])*weightCacheCreation +
		float64(r[2])*weightCacheRead + float64(r[3])*weightOutput
}

func costs(ss []ProcessSample) []float64 {
	out := make([]float64, len(ss))
	for i, s := range ss {
		out[i] = weighted(s.Raw)
	}
	return out
}

// returns is 1/0 per delivery with a Gauntlet record (crit. 4-5).
func returns(ss []ProcessSample) []float64 {
	var out []float64
	for _, s := range ss {
		if s.HasGauntlet {
			out = append(out, bit(s.Returned))
		}
	}
	return out
}

// escapes is 1/0 per delivery: a fix created in (completion, completion+28d]
// (crit. 19) — one created before completion is in-cycle rework.
func escapes(ss []ProcessSample) []float64 {
	out := make([]float64, len(ss))
	for i, s := range ss {
		for _, t := range s.Fixes {
			if t.After(s.CompletedAt) && !t.After(s.CompletedAt.Add(window)) {
				out[i] = 1
				break
			}
		}
	}
	return out
}

func bit(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func mean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

// median is the middle value, or the mean of the two middle ones (the rule of
// medianAndP90).
func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := slices.Clone(v)
	slices.Sort(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// quantile is the nearest-rank p-quantile: the ceil(p·n)-th smallest value.
func quantile(v []float64, p float64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	i := int(math.Ceil(p*float64(len(s)))) - 1
	return s[max(0, min(i, len(s)-1))]
}
