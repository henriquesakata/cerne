package stats

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"
	"time"
)

// day0 is a Monday (2026-W02), so at(n) lands on known ISO weeks.
var day0 = time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

func at(day int) time.Time { return day0.AddDate(0, 0, day) }

// mature is a "now" far past every horizon: the escaped guard is never pending.
var mature = at(10_000)

// sample is a stamped delivery whose weighted cost is exactly cost (all of it
// as input, weight 1), with a Gauntlet record and no defeat.
func sample(id string, t time.Time, cost float64) ProcessSample {
	return ProcessSample{ID: id, CompletedAt: t, Raw: [4]int64{int64(math.Round(cost)), 0, 0, 0}, HasGauntlet: true}
}

// spread is n deterministic deliveries log-uniform around median (factor e
// either way), dated evenly across one window starting at from.
func spread(median float64, n int, from time.Time) []ProcessSample {
	out := make([]ProcessSample, n)
	for i := range out {
		c := median * math.Exp(-1+2*float64(i)/float64(n-1))
		out[i] = sample(fmt.Sprintf("S-%s-%03d", from.Format("20060102"), i), from.Add(time.Duration(i)*window/time.Duration(n)), c)
	}
	return out
}

var digitRe = regexp.MustCompile(`\d`)

// assertReason pins criterion 13: a verdict always carries a one-line reason
// with the numbers behind it, never the bare word.
func assertReason(t *testing.T, m MarkerReport) {
	t.Helper()
	if m.Reason == "" || strings.Contains(m.Reason, "\n") || !digitRe.MatchString(m.Reason) {
		t.Fatalf("reason = %q, want one line carrying its numbers", m.Reason)
	}
}

func marker(id string, t time.Time) Marker { return Marker{ID: id, Title: "mudança " + id, At: t} }

// TestWeightedCostMatchesFormula pins criterion 1: the weighted cost of the
// real F-0309 anchor is input + 1.25·cache_creation + 0.1·cache_read + 5·output.
func TestWeightedCostMatchesFormula(t *testing.T) {
	s := ProcessSample{ID: "F-0309", CompletedAt: at(0), Raw: [4]int64{302, 339857, 28934595, 150077}}
	r := ComputeProcess([]ProcessSample{s}, nil, mature)
	if len(r.Weeks) != 1 || r.Weeks[0].Median != 4068967.75 {
		t.Fatalf("weeks = %+v, want one week with median 4068967.75", r.Weeks)
	}
}

// TestProcessWeeklyMedianAndN pins criterion 3: one entry per ISO week (UTC)
// with the median and n of that week's deliveries; Sunday 23:59 is still the
// week, Monday 00:00 is the next; a week with no delivery is not emitted.
func TestProcessWeeklyMedianAndN(t *testing.T) {
	samples := []ProcessSample{
		sample("A", at(0), 1),
		sample("B", at(1), 3),
		sample("C", at(2), 10),
		sample("D", time.Date(2026, 1, 11, 23, 59, 0, 0, time.UTC), 7),
		sample("E", time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC), 7),
		sample("F", at(21), 2),
	}
	r := ComputeProcess(samples, nil, mature)
	want := []WeekCost{
		{Week: "2026-W02", Start: at(0), Median: 5, N: 4},
		{Week: "2026-W03", Start: at(7), Median: 7, N: 1},
		{Week: "2026-W05", Start: at(21), Median: 2, N: 1},
	}
	if len(r.Weeks) != len(want) {
		t.Fatalf("weeks = %+v, want %+v", r.Weeks, want)
	}
	for i, w := range want {
		g := r.Weeks[i]
		if g.Week != w.Week || !g.Start.Equal(w.Start) || g.Median != w.Median || g.N != w.N {
			t.Fatalf("week %d = %+v, want %+v", i, g, w)
		}
	}
}

// TestProcessWindowsTruncateAtNeighbour pins criterion 7: before = [m−28d, m),
// after = [m, m+28d), each cut at the neighbouring marker and naming it.
func TestProcessWindowsTruncateAtNeighbour(t *testing.T) {
	var samples []ProcessSample
	for _, d := range []int{0, 10, 29, 30, 35, 39, 40, 50, 70} {
		samples = append(samples, sample(fmt.Sprintf("D%02d", d), at(d), float64(d+1)))
	}
	r := ComputeProcess(samples, []Marker{marker("B", at(40)), marker("A", at(30))}, mature)
	if len(r.Markers) != 2 || r.Markers[0].ID != "A" || r.Markers[1].ID != "B" {
		t.Fatalf("markers = %+v, want A then B", r.Markers)
	}
	a, b := r.Markers[0], r.Markers[1]
	check := func(name string, s Side, n int, med float64, from, to time.Time, cut string) {
		t.Helper()
		if s.N != n || s.Median != med || !s.From.Equal(from) || !s.To.Equal(to) || s.CutBy != cut {
			t.Fatalf("%s = n %d median %v [%s, %s) cut %q, want n %d median %v [%s, %s) cut %q",
				name, s.N, s.Median, s.From, s.To, s.CutBy, n, med, from, to, cut)
		}
	}
	check("A.before", a.Before, 2, 20.5, at(2), at(30), "") // days 10, 29
	check("A.after", a.After, 3, 36, at(30), at(40), "B")   // days 30, 35, 39
	check("B.before", b.Before, 3, 36, at(30), at(40), "A") // days 30, 35, 39
	check("B.after", b.After, 2, 46, at(40), at(68), "")    // days 40, 50
}

// TestProcessSideStats pins criteria 8 and 1: per side, n, median, p25, p75 of
// the weighted cost, the median of each raw counter, the share with a return
// (of those with a Gauntlet) and the share with an escaped defect (of n).
func TestProcessSideStats(t *testing.T) {
	var samples []ProcessSample
	for i := range 8 {
		k := int64(i + 1)
		s := ProcessSample{ID: fmt.Sprintf("B%d", k), CompletedAt: at(10 + i), Raw: [4]int64{k, 10 * k, 100 * k, 2 * k}}
		s.HasGauntlet = i < 6 // 6 with a Gauntlet record
		s.Returned = i%2 == 0 // returned: 0, 2, 4 among them (6 is out of the guard)
		if i == 1 || i == 3 { // two escaped defects, fix created 5 days after delivery
			s.Fixes = []time.Time{s.CompletedAt.AddDate(0, 0, 5)}
		}
		if i == 5 { // a fix created before completion is in-cycle rework, never escaped
			s.Fixes = []time.Time{s.CompletedAt.AddDate(0, 0, -1)}
		}
		samples = append(samples, s)
	}
	samples = append(samples, spread(1000, 8, at(30))...)
	r := ComputeProcess(samples, []Marker{marker("D-0001", at(30))}, mature)
	b := r.Markers[0].Before
	// weighted = k·(1 + 12.5 + 10 + 10) = 33.5k, k = 1..8
	if b.N != 8 || b.Median != 150.75 || b.P25 != 67 || b.P75 != 201 {
		t.Fatalf("before = n %d median %v p25 %v p75 %v, want 8 / 150.75 / 67 / 201", b.N, b.Median, b.P25, b.P75)
	}
	if want := (Raw{Input: 4.5, CacheCreation: 45, CacheRead: 450, Output: 9}); b.RawMedian != want {
		t.Fatalf("raw median = %+v, want %+v", b.RawMedian, want)
	}
	if b.WithGauntlet != 6 || b.ReturnedShare != 0.5 || b.EscapedShare == nil || *b.EscapedShare != 0.25 {
		t.Fatalf("guards = with %d returned %v escaped %v, want 6 / 0.5 / 0.25", b.WithGauntlet, b.ReturnedShare, b.EscapedShare)
	}
	if a := r.Markers[0].After; a.N != 8 || a.WithGauntlet != 8 {
		t.Fatalf("after = n %d with %d, want 8 / 8", a.N, a.WithGauntlet)
	}
	if r.ExcludedNoGauntlet != 2 {
		t.Fatalf("excluded_no_gauntlet = %d, want 2", r.ExcludedNoGauntlet)
	}
}

// TestProcessSmallSampleInconclusive pins criterion 9: any side under 8 is
// inconclusivo for a small sample — no interval, no guard computed.
func TestProcessSmallSampleInconclusive(t *testing.T) {
	samples := append(spread(20e6, 7, at(2)), spread(20e6, 30, at(30))...)
	m := ComputeProcess(samples, []Marker{marker("D-0001", at(30))}, mature).Markers[0]
	if m.Verdict != "inconclusivo" || m.Code != "small_sample" || !strings.HasPrefix(m.Reason, "small sample (n=7 before, n=30 after") {
		t.Fatalf("marker = %s/%s %q, want inconclusivo/small_sample small sample (n=7 before, n=30 after…", m.Verdict, m.Code, m.Reason)
	}
	if m.CostRatioCI != nil || m.Cost != "" || m.Returned != "" || m.Escaped != "" {
		t.Fatalf("marker = %+v, want no interval and no guard", m)
	}
	assertReason(t, m)
}

// TestProcessCostVerdictBootstrap pins criterion 10: with n >= 8 per side the
// 90% bootstrap interval of the median ratio decides — wholly below 1 is
// melhorou, wholly above is piorou, straddling 1 is noise.
func TestProcessCostVerdictBootstrap(t *testing.T) {
	cases := []struct {
		name        string
		afterMedian float64
		verdict     string
		code        string
	}{
		{"40% down", 12e6, "melhorou", "cost"},
		{"no change", 20e6, "inconclusivo", "within_noise"},
		// The log-mirror of −40% (×1/0.6, +67%): a plain +40% is a smaller
		// log shift, and with this e^±1 dispersion at n=30 its interval is
		// 0.90–2.12 — honestly within noise, as the rule should say.
		{"40% down mirrored up", 20e6 / 0.6, "piorou", "cost"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			samples := append(spread(20e6, 30, at(2)), spread(tc.afterMedian, 30, at(30))...)
			m := ComputeProcess(samples, []Marker{marker("D-0001", at(30))}, mature).Markers[0]
			if m.Verdict != tc.verdict || m.Code != tc.code || len(m.CostRatioCI) != 2 {
				t.Fatalf("marker = %s/%s ci %v (%q), want %s/%s", m.Verdict, m.Code, m.CostRatioCI, m.Reason, tc.verdict, tc.code)
			}
			lo, hi := m.CostRatioCI[0], m.CostRatioCI[1]
			switch tc.verdict {
			case "melhorou":
				if hi >= 1 {
					t.Fatalf("ci = %v, want wholly below 1", m.CostRatioCI)
				}
			case "piorou":
				if lo <= 1 {
					t.Fatalf("ci = %v, want wholly above 1", m.CostRatioCI)
				}
			default:
				// crit. 10's literal reason, then (C15) what the interval
				// cannot tell apart, with the sample sizes: low power is not "no effect".
				if !(lo < 1 && 1 < hi) || !strings.HasPrefix(m.Reason, "difference within noise: median ratio") ||
					!strings.Contains(m.Reason, "changes in that range are not distinguishable with n=30 before, n=30 after") {
					t.Fatalf("ci = %v reason %q, want 1 inside and what the interval cannot distinguish", m.CostRatioCI, m.Reason)
				}
			}
			assertReason(t, m)
		})
	}
}

// TestProcessBootstrapReproducible pins criterion 10's fixed seed: the same
// input gives the same intervals, and a distant extra marker does not move them.
func TestProcessBootstrapReproducible(t *testing.T) {
	before, after := spread(20e6, 30, at(2)), spread(15e6, 30, at(30))
	for i := range after {
		after[i].Returned = i%3 == 0
		before[i].Returned = i%4 == 0
	}
	samples := append(before, after...)
	one := ComputeProcess(samples, []Marker{marker("D-0001", at(30))}, mature).Markers[0]
	two := ComputeProcess(samples, []Marker{marker("D-0001", at(30))}, mature).Markers[0]
	far := ComputeProcess(samples, []Marker{marker("D-0001", at(30)), marker("D-0002", at(400))}, mature).Markers[0]
	for _, m := range []MarkerReport{two, far} {
		if fmt.Sprint(m.CostRatioCI, m.ReturnedDiffCI, m.EscapedDiffCI) != fmt.Sprint(one.CostRatioCI, one.ReturnedDiffCI, one.EscapedDiffCI) {
			t.Fatalf("ci = %v %v %v, want %v %v %v", m.CostRatioCI, m.ReturnedDiffCI, m.EscapedDiffCI,
				one.CostRatioCI, one.ReturnedDiffCI, one.EscapedDiffCI)
		}
	}
	if len(one.CostRatioCI) != 2 || len(one.ReturnedDiffCI) != 2 {
		t.Fatalf("ci = %v %v, want both computed", one.CostRatioCI, one.ReturnedDiffCI)
	}
}

// TestProcessReturnedGuard pins criterion 11: the 90% interval of the change
// in the share of returned deliveries; wholly above 0 is piorou (and vetoes);
// too few Gauntlet records on a side is inconclusivo and never vetoes.
func TestProcessReturnedGuard(t *testing.T) {
	build := func(afterReturned, afterGauntlet, beforeReturned func(i int) bool) MarkerReport {
		before, after := spread(20e6, 30, at(2)), spread(20e6, 30, at(30))
		for i := range after {
			after[i].Returned, after[i].HasGauntlet = afterReturned(i), afterGauntlet(i)
			before[i].Returned = beforeReturned(i)
		}
		return ComputeProcess(append(before, after...), []Marker{marker("D-0001", at(30))}, mature).Markers[0]
	}
	all := func(int) bool { return true }
	none := func(int) bool { return false }
	half := func(i int) bool { return i%2 == 0 }
	sixth := func(i int) bool { return i%6 == 0 }

	m := build(half, all, none)
	if m.Returned != "piorou" || len(m.ReturnedDiffCI) != 2 || m.ReturnedDiffCI[0] <= 0 {
		t.Fatalf("returned = %s ci %v, want piorou wholly above 0", m.Returned, m.ReturnedDiffCI)
	}
	if m.Verdict != "piorou" || m.Code != "quality_worse_cost_flat" {
		t.Fatalf("marker = %s/%s %q, want piorou/quality_worse_cost_flat", m.Verdict, m.Code, m.Reason)
	}
	assertReason(t, m)

	m = build(half, func(i int) bool { return i < 5 }, none)
	if m.Returned != "inconclusivo" || m.ReturnedDiffCI != nil || m.Verdict != "inconclusivo" || m.Code != "within_noise" {
		t.Fatalf("thin guard = returned %s ci %v final %s/%s, want inconclusivo, no veto", m.Returned, m.ReturnedDiffCI, m.Verdict, m.Code)
	}

	m = build(sixth, all, sixth)
	if m.Returned != "ok" || m.Verdict != "inconclusivo" || m.Code != "within_noise" {
		t.Fatalf("equal returns = returned %s final %s/%s, want ok and no veto", m.Returned, m.Verdict, m.Code)
	}
	assertReason(t, m)
}

// TestProcessQualityGuardVetoesCheaper pins criteria 12 and 19: a cheaper
// change whose quality guard got worse — by returns or by escaped defects — is
// piorou for the quality guard.
func TestProcessQualityGuardVetoesCheaper(t *testing.T) {
	before, after := spread(20e6, 30, at(2)), spread(12e6, 30, at(30))
	for i := range after {
		after[i].Returned = i%2 == 0
	}
	m := ComputeProcess(append(before, after...), []Marker{marker("D-0001", at(30))}, mature).Markers[0]
	if m.Cost != "melhorou" || m.Verdict != "piorou" || m.Code != "quality_guard" || !strings.Contains(m.Reason, "quality guard") {
		t.Fatalf("returns veto = cost %s final %s/%s %q, want melhorou → piorou/quality_guard", m.Cost, m.Verdict, m.Code, m.Reason)
	}
	assertReason(t, m)

	before, after = spread(20e6, 30, at(2)), spread(12e6, 30, at(30))
	for i := range after {
		if i%2 == 0 {
			after[i].Fixes = []time.Time{after[i].CompletedAt.AddDate(0, 0, 10)}
		}
	}
	m = ComputeProcess(append(before, after...), []Marker{marker("D-0001", at(30))}, mature).Markers[0]
	if m.Cost != "melhorou" || m.Escaped != "piorou" || m.Verdict != "piorou" || m.Code != "quality_guard" {
		t.Fatalf("escaped veto = cost %s escaped %s final %s/%s %q, want melhorou/piorou → piorou/quality_guard",
			m.Cost, m.Escaped, m.Verdict, m.Code, m.Reason)
	}
	assertReason(t, m)
}

// TestProcessEscapedDefectHorizon pins criterion 19: a fix created in
// (completion, completion+28d] is an escaped defect; one created before the
// completion is in-cycle rework, one after the horizon does not count.
func TestProcessEscapedDefectHorizon(t *testing.T) {
	cases := []struct {
		fixDay int
		want   float64
	}{{-1, 0}, {10, 1}, {28, 1}, {30, 0}}
	for _, tc := range cases {
		s := sample("F-0001", at(0), 100)
		s.Fixes = []time.Time{at(tc.fixDay)}
		r := ComputeProcess([]ProcessSample{s}, []Marker{marker("D-0001", at(-5))}, mature)
		if got := r.Markers[0].After.EscapedShare; got == nil || *got != tc.want {
			t.Fatalf("fix on day %d: escaped share = %v, want %v", tc.fixDay, got, tc.want)
		}
	}
}

// TestProcessEscapedGuardPending pins criterion 20: until 28 days have passed
// since the last delivery of the after window, the escaped guard is pending
// and out of the verdict.
func TestProcessEscapedGuardPending(t *testing.T) {
	before, after := spread(20e6, 30, at(2)), spread(12e6, 30, at(30))
	for i := range after {
		if i%2 == 0 {
			after[i].Fixes = []time.Time{after[i].CompletedAt.AddDate(0, 0, 10)}
		}
	}
	last := after[len(after)-1].CompletedAt
	samples := append(before, after...)

	m := ComputeProcess(samples, []Marker{marker("D-0001", at(30))}, last.AddDate(0, 0, 10)).Markers[0]
	if m.Escaped != "pending" || m.EscapedDiffCI != nil || m.Verdict != "melhorou" || !strings.Contains(m.Reason, "pending until") {
		t.Fatalf("young = escaped %s ci %v final %s %q, want pending, no interval, melhorou", m.Escaped, m.EscapedDiffCI, m.Verdict, m.Reason)
	}
	assertReason(t, m)

	m = ComputeProcess(samples, []Marker{marker("D-0001", at(30))}, last.AddDate(0, 0, 29)).Markers[0]
	if m.Escaped != "piorou" || m.Verdict != "piorou" {
		t.Fatalf("mature = escaped %s final %s, want piorou/piorou", m.Escaped, m.Verdict)
	}
}

// TestProcessEmptyHasReason pins criterion 16 in the pure layer: no stamped
// delivery at all gives the explicit reason, an empty (never null) series, and
// the markers still listed as a small sample.
func TestProcessEmptyHasReason(t *testing.T) {
	var unstamped []ProcessSample
	for i := range 3 {
		unstamped = append(unstamped, ProcessSample{ID: fmt.Sprintf("U%d", i), CompletedAt: at(i), Excluded: "no_cost"})
	}
	r := ComputeProcess(unstamped, []Marker{marker("D-0001", at(0))}, mature)
	if !strings.Contains(r.Reason, "F-0309") || r.Weeks == nil || len(r.Weeks) != 0 || r.ExcludedNoCost != 3 {
		t.Fatalf("report = %+v, want the F-0309 reason, empty weeks, excluded 3", r)
	}
	b, _ := json.Marshal(r)
	if !strings.Contains(string(b), `"weeks":[]`) {
		t.Fatalf("json = %s, want weeks: []", b)
	}
	m := r.Markers[0]
	if m.Verdict != "inconclusivo" || !strings.HasPrefix(m.Reason, "small sample (n=0 before, n=0 after") {
		t.Fatalf("marker = %s %q, want inconclusivo small sample (n=0 before, n=0 after…", m.Verdict, m.Reason)
	}
	if r := ComputeProcess(nil, nil, mature); r.Markers == nil {
		t.Fatalf("markers = nil, want []")
	}
}

func bits(k, n int) []float64 {
	out := make([]float64, n)
	for i := range k {
		out[i] = 1
	}
	return out
}

// TestProcessGuardScoreInterval pins C7: a guard's interval is Newcombe's
// hybrid score interval (method 10), which keeps its width when one side is
// all zero — 0/10 vs 3/10 at 90% is +2.6 to +55.8 pp, where a percentile
// bootstrap claimed +10 to +50.
func TestProcessGuardScoreInterval(t *testing.T) {
	ci := newcombe(bits(0, 10), bits(3, 10), 1.6448536269514722)
	if math.Abs(ci[0]-0.0255) > 0.0005 || math.Abs(ci[1]-0.5583) > 0.0005 {
		t.Fatalf("newcombe(0/10, 3/10, 90%%) = %v, want ≈ [0.0255, 0.5583]", ci)
	}
	var samples []ProcessSample
	for i := range 10 {
		b, a := sample(fmt.Sprintf("B%d", i), at(5+i), 1000), sample(fmt.Sprintf("A%d", i), at(30+i), 1000)
		a.Returned = i < 3
		samples = append(samples, b, a)
	}
	m := ComputeProcess(samples, []Marker{marker("D-0001", at(30))}, mature).Markers[0]
	if want := newcombe(bits(0, 10), bits(3, 10), guardZ); fmt.Sprint(m.ReturnedDiffCI) != fmt.Sprint(want) {
		t.Fatalf("returned ci = %v, want the score interval %v", m.ReturnedDiffCI, want)
	}
}

// TestProcessGuardsJudgeBothWays pins C1 and C20: with the cost unchanged, a
// guard that got measurably better says melhorou and one that got worse says
// piorou, each under its own code; worse cost and worse quality says both.
func TestProcessGuardsJudgeBothWays(t *testing.T) {
	build := func(afterMedian float64, beforeReturned, afterReturned func(int) bool) MarkerReport {
		before, after := spread(20e6, 30, at(2)), spread(afterMedian, 30, at(30))
		for i := range 30 {
			before[i].Returned, after[i].Returned = beforeReturned(i), afterReturned(i)
		}
		return ComputeProcess(append(before, after...), []Marker{marker("D-0001", at(30))}, mature).Markers[0]
	}
	none := func(int) bool { return false }
	most := func(i int) bool { return i%5 != 0 }

	m := build(20e6, most, none)
	if m.Cost != "inconclusivo" || m.Returned != "melhorou" || m.Verdict != "melhorou" || m.Code != "quality_better_cost_flat" ||
		!strings.HasPrefix(m.Reason, "quality better, no detectable cost difference") {
		t.Fatalf("better quality = cost %s returned %s final %s/%s %q, want melhorou/quality_better_cost_flat", m.Cost, m.Returned, m.Verdict, m.Code, m.Reason)
	}
	m = build(20e6, none, most)
	if m.Returned != "piorou" || m.Verdict != "piorou" || m.Code != "quality_worse_cost_flat" ||
		!strings.HasPrefix(m.Reason, "quality worse, no detectable cost difference") || strings.Contains(m.Reason, "vetoes") {
		t.Fatalf("worse quality = returned %s final %s/%s %q, want piorou/quality_worse_cost_flat, never a veto", m.Returned, m.Verdict, m.Code, m.Reason)
	}
	m = build(20e6/0.6, none, most)
	if m.Cost != "piorou" || m.Verdict != "piorou" || m.Code != "cost_and_quality_worse" || !strings.Contains(m.Reason, "cost up") || !strings.Contains(m.Reason, "returned share") {
		t.Fatalf("both worse = cost %s final %s/%s %q, want piorou/cost_and_quality_worse naming both", m.Cost, m.Verdict, m.Code, m.Reason)
	}
	assertReason(t, m)
}

// TestProcessNullCalibration pins C2: with the same distribution on both sides
// (lognormal σ=1 costs, 20% returns, 10% escapes, fixed seed), the panel says
// piorou or melhorou rarely and about as often. Tolerance: each rate ≤ 6.5%
// — the cost test's 5% per tail plus the guards' Bonferroni share, measured
// ≈ 4–6%, plus ~2 Monte Carlo SE (0.7 pp at 1000 trials) — and the two within
// 2.5 pp of each other. Without the multiplicity control piorou ran 10–12%.
func TestProcessNullCalibration(t *testing.T) {
	far := at(10_000)
	for _, n := range []int{12, 30} {
		rng := rand.New(rand.NewPCG(7, 11))
		const trials = 1000
		count := map[string]int{}
		for range trials {
			var ss []ProcessSample
			for side := range 2 {
				for i := range n {
					t := at(2 + 28*side).Add(time.Duration(i) * window / time.Duration(n))
					s := ProcessSample{ID: fmt.Sprintf("F-%d-%03d", side, i), CompletedAt: t, HasGauntlet: true,
						Raw: [4]int64{int64(math.Exp(15 + rng.NormFloat64())), 0, 0, 0}, Returned: rng.Float64() < 0.2}
					if rng.Float64() < 0.1 {
						s.Fixes = []time.Time{t.Add(48 * time.Hour)}
					}
					ss = append(ss, s)
				}
			}
			count[ComputeProcess(ss, []Marker{marker("D-0001", at(30))}, far).Markers[0].Verdict]++
		}
		worse, better := 100*float64(count["piorou"])/trials, 100*float64(count["melhorou"])/trials
		t.Logf("null n=%d: piorou %.1f%% melhorou %.1f%%", n, worse, better)
		if worse > 6.5 || better > 6.5 || math.Abs(worse-better) > 2.5 {
			t.Fatalf("null n=%d: piorou %.1f%% melhorou %.1f%%, want each ≤ 6.5%% and within 2.5 pp", n, worse, better)
		}
	}
}

// TestProcessUnguardedVerdictIsQualified pins C9: a verdict whose guards were
// not both evaluated (pending or too thin) carries guarded=false, so a bare
// melhorou never reads like a guarded one.
func TestProcessUnguardedVerdictIsQualified(t *testing.T) {
	before, after := spread(20e6, 30, at(2)), spread(12e6, 30, at(30))
	for i := range 30 {
		before[i].HasGauntlet, after[i].HasGauntlet = i < 3, i < 3
	}
	young := after[29].CompletedAt.AddDate(0, 0, 10)
	m := ComputeProcess(append(before, after...), []Marker{marker("D-0001", at(30))}, young).Markers[0]
	if m.Verdict != "melhorou" || m.Guarded || m.Returned != "inconclusivo" || m.Escaped != "pending" {
		t.Fatalf("unguarded = %s guarded %v returned %s escaped %s, want melhorou guarded=false", m.Verdict, m.Guarded, m.Returned, m.Escaped)
	}
	b, _ := json.Marshal(m)
	if !strings.Contains(string(b), `"guarded":false`) {
		t.Fatalf("json = %s, want guarded:false", b)
	}
	before, after = spread(20e6, 30, at(2)), spread(12e6, 30, at(30))
	if m := ComputeProcess(append(before, after...), []Marker{marker("D-0001", at(30))}, mature).Markers[0]; m.Verdict != "melhorou" || !m.Guarded {
		t.Fatalf("guarded = %s guarded %v, want melhorou guarded=true", m.Verdict, m.Guarded)
	}
}

// TestProcessWindowCutHasItsOwnReason pins C3: markers 2h apart cut each
// other's facing window to 2h, which can never hold 8 deliveries — the reason
// says so, naming the neighbour and the real window, never "wait for data".
func TestProcessWindowCutHasItsOwnReason(t *testing.T) {
	a, b := at(30), at(30).Add(2*time.Hour+8*time.Minute)
	samples := append(spread(20e6, 10, at(2)), spread(12e6, 10, b)...)
	r := ComputeProcess(samples, []Marker{marker("D-0061", a), marker("D-0062", b)}, mature)
	m := r.Markers[0]
	if m.Verdict != "inconclusivo" || m.Code != "window_cut" || m.CutSide != "after" ||
		!strings.Contains(m.Reason, "after window cut at D-0062: 2026-02-04 00:00 → 2026-02-04 02:08 UTC (2h8m, n=0)") ||
		!strings.Contains(m.Reason, "cannot get a verdict") || strings.Contains(m.Reason, "needed") {
		t.Fatalf("D-0061 = %s/%s side %q %q, want window_cut on after, naming D-0062 and the 2h window", m.Verdict, m.Code, m.CutSide, m.Reason)
	}
	if m := r.Markers[1]; m.Code != "window_cut" || m.CutSide != "before" || !strings.Contains(m.Reason, "before window cut at D-0061") {
		t.Fatalf("D-0062 = %s side %q %q, want window_cut on before, naming D-0061", m.Code, m.CutSide, m.Reason)
	}
	assertReason(t, m)
}

// TestProcessIntervalLevelDecides pins C11's level: a 20% drop at n=30 with
// this dispersion clears a 50% interval but not the 90% one, so the verdict
// must stay inconclusive — a narrower interval would claim melhorou.
func TestProcessIntervalLevelDecides(t *testing.T) {
	samples := append(spread(20e6, 30, at(2)), spread(16e6, 30, at(30))...)
	m := ComputeProcess(samples, []Marker{marker("D-0001", at(30))}, mature).Markers[0]
	if m.Verdict != "inconclusivo" || len(m.CostRatioCI) != 2 || m.CostRatioCI[1] <= 1 || m.CostRatioCI[0] > 0.7 {
		t.Fatalf("−20%% = %s ci %v, want inconclusivo with a 90%% interval reaching past 1 and below 0.7", m.Verdict, m.CostRatioCI)
	}
}

// TestProcessComparesMedians pins C11's statistic: the after side is the
// before side with its 12 dearest deliveries ×10 — the mean ratio is ~5, the
// median ratio exactly 1 — so the verdict compares medians, not means.
func TestProcessComparesMedians(t *testing.T) {
	before, after := spread(20e6, 30, at(2)), spread(20e6, 30, at(30))
	for i := 18; i < 30; i++ {
		after[i].Raw[0] *= 10
	}
	m := ComputeProcess(append(before, after...), []Marker{marker("D-0001", at(30))}, mature).Markers[0]
	if m.Verdict != "inconclusivo" || m.After.Median != m.Before.Median || !(m.CostRatioCI[0] < 1 && 1 < m.CostRatioCI[1]) {
		t.Fatalf("heavy tail = %s medians %v/%v ci %v, want inconclusivo with 1 inside the median-ratio interval",
			m.Verdict, m.Before.Median, m.After.Median, m.CostRatioCI)
	}
}

// TestProcessImmatureEscapedShareWithheld pins C10: a side whose last
// delivery is under 28 days old has no escaped share yet (null), since its
// fixes have not had time to happen; a mature side keeps its share.
func TestProcessImmatureEscapedShareWithheld(t *testing.T) {
	before, after := spread(20e6, 30, at(2)), spread(12e6, 30, at(30))
	young := after[29].CompletedAt.AddDate(0, 0, 10)
	m := ComputeProcess(append(before, after...), []Marker{marker("D-0001", at(30))}, young).Markers[0]
	if m.After.EscapedShare != nil || m.Before.EscapedShare == nil {
		t.Fatalf("escaped shares = before %v after %v, want before kept and after withheld", m.Before.EscapedShare, m.After.EscapedShare)
	}
	if b, _ := json.Marshal(m.After); !strings.Contains(string(b), `"escaped_share":null`) {
		t.Fatalf("after json = %s, want escaped_share null", b)
	}
}

// TestProcessExclusionsPerSide pins C13: deliveries and exclusions are counted
// per side and per week, so a side whose stamped subset is thin or skewed
// shows it next to its numbers.
func TestProcessExclusionsPerSide(t *testing.T) {
	unstamped := func(id string, t time.Time) ProcessSample {
		return ProcessSample{ID: id, CompletedAt: t, Excluded: "no_cost"}
	}
	noGauntlet := sample("A1", at(31), 100)
	noGauntlet.HasGauntlet = false
	samples := []ProcessSample{
		sample("B1", at(10), 100), unstamped("B2", at(11)), unstamped("B3", at(12)),
		noGauntlet, unstamped("A2", at(32)),
	}
	r := ComputeProcess(samples, []Marker{marker("D-0001", at(30))}, mature)
	b, a := r.Markers[0].Before, r.Markers[0].After
	if b.Delivered != 3 || b.N != 1 || b.ExcludedNoCost != 2 || b.ExcludedNoGauntlet != 0 {
		t.Fatalf("before = delivered %d n %d no_cost %d no_gauntlet %d, want 3 / 1 / 2 / 0", b.Delivered, b.N, b.ExcludedNoCost, b.ExcludedNoGauntlet)
	}
	if a.Delivered != 2 || a.N != 1 || a.ExcludedNoCost != 1 || a.ExcludedNoGauntlet != 1 {
		t.Fatalf("after = delivered %d n %d no_cost %d no_gauntlet %d, want 2 / 1 / 1 / 1", a.Delivered, a.N, a.ExcludedNoCost, a.ExcludedNoGauntlet)
	}
	if r.ExcludedNoCost != 3 || r.ExcludedNoGauntlet != 1 {
		t.Fatalf("totals = no_cost %d no_gauntlet %d, want 3 / 1", r.ExcludedNoCost, r.ExcludedNoGauntlet)
	}
	if len(r.Weeks) != 2 || r.Weeks[0].Delivered != 3 || r.Weeks[0].N != 1 || r.Weeks[1].Delivered != 2 || r.Weeks[1].N != 1 {
		t.Fatalf("weeks = %+v, want W03 delivered 3 n 1 and W06 delivered 2 n 1", r.Weeks)
	}
}

// TestProcessSideCoverage pins C5: a side shows how much of its bound
// sessions' spend the anchors attribute — the median unattributed and the
// coverage, bound / (bound + unattributed), in weighted tokens.
func TestProcessSideCoverage(t *testing.T) {
	var samples []ProcessSample
	for i := range 4 {
		s := sample(fmt.Sprintf("A%d", i), at(31+i), 300)
		s.Unattributed = [4]int64{int64(100 * i), 0, 0, 0} // 0, 100, 200, 300
		samples = append(samples, s)
	}
	a := ComputeProcess(samples, []Marker{marker("D-0001", at(30))}, mature).Markers[0].After
	// bound 1200, unattributed 600: coverage 2/3; unattributed median (100+200)/2
	if a.UnattributedMedian != 150 || math.Abs(a.Coverage-2.0/3) > 1e-12 {
		t.Fatalf("after = unattributed median %v coverage %v, want 150 / 0.667", a.UnattributedMedian, a.Coverage)
	}
}

// TestProcessReportCarriesItsRule: the card states the n a side needs (C19)
// and the guards' level from the report, never from a copy of the constants.
func TestProcessReportCarriesItsRule(t *testing.T) {
	if r := ComputeProcess(nil, nil, mature); r.MinSample != 8 || r.GuardLevel != 0.975 {
		t.Fatalf("report = min_sample %d guard_level %v, want 8 / 0.975", r.MinSample, r.GuardLevel)
	}
}
