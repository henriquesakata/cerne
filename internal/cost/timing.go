package cost

import (
	"slices"
	"sort"
	"strings"
	"time"
)

// maxPause caps what one pause may add to the active time (F-0528 crit. 1): a
// longer one is the machine asleep or a tool stuck, not work. 14 minutes is the
// slowest suite of this repo under load (F-0289), so 30 clears it.
// ponytail: a constant, not a setting; make it one if a project's tools run longer.
const maxPause = 30 * time.Minute

// The lifecycle phases the active time is split into (F-0528 crit. 4).
const (
	phaseSpec    = "spec"
	phasePlan    = "plano"
	phaseDev     = "implementação"
	phaseReview  = "revisão"
	phaseNone    = "sem fase"
	timeKeySplit = "/" // fileAgg.Time keys: phase + "/" + activity
)

// lifeEvents are the events that move a feature between phases.
var lifeEvents = map[string]bool{
	"feature.created": true, "feature.approved": true, "feature.started": true,
	"feature.completed": true, "feature.accepted": true,
}

// Timing is the measured working time of one entity, in seconds (F-0528):
// the pauses between the lines of its main-thread sessions, split by lifecycle
// phase and by activity, apart from the wait for the human and from the
// subagents' time. nil means not measured, never zero.
type Timing struct {
	ActiveSeconds    int64            `json:"active_seconds"`
	WallSeconds      int64            `json:"wall_seconds"`
	HumanWaitSeconds int64            `json:"human_wait_seconds"`
	ByPhase          map[string]int64 `json:"by_phase"`
	ByActivity       map[string]int64 `json:"by_activity"`
	Sidechain        *SidechainTime   `json:"sidechain_time,omitempty"`
	spans            [][2]time.Time   // the main-thread active intervals, united; not serialized
}

// SidechainTime is the subagents' active time, measured by the same rules and
// kept out of the main thread's.
type SidechainTime struct {
	ActiveSeconds int64            `json:"active_seconds"`
	ByActivity    map[string]int64 `json:"by_activity"`
}

// phase is the lifecycle phase of entity at t: by the earliest event of each
// kind; sem fase when the entity has none (a W- without a feature, an old
// backend).
func (a attributor) phase(entity string, t time.Time) string {
	ev := a.life[entity]
	if len(ev) == 0 {
		return phaseNone
	}
	reached := func(types ...string) bool {
		for _, ty := range types {
			if at, ok := ev[ty]; ok && !t.Before(at) {
				return true
			}
		}
		return false
	}
	switch {
	case reached("feature.completed", "feature.accepted"):
		return phaseReview
	case reached("feature.started"):
		return phaseDev
	case reached("feature.approved"):
		return phasePlan
	}
	return phaseSpec
}

// addTimes measures the pauses of one session's ticks into agg, per
// (entity, session, file): each file's ticks in time order, the pause up to a
// line counted to the entity governing that line, to the phase and activity
// of that line. want says which entities are collected.
func addTimes(agg map[aggKey]*fileAgg, att attributor, binds []Bind, spawn map[string]time.Time, sess string, ticks []tick, want func(entity string) bool) {
	byFile := map[string][]tick{}
	var files []string
	for _, k := range ticks {
		if _, ok := byFile[k.m.file]; !ok {
			files = append(files, k.m.file)
		}
		byFile[k.m.file] = append(byFile[k.m.file], k)
	}
	sort.Strings(files)
	for _, file := range files {
		ts := byFile[file]
		sort.SliceStable(ts, func(i, j int) bool { return ts[i].m.ts.Before(ts[j].m.ts) })
		sub := strings.Contains(file, "/subagents/")
		touched := map[string]*fileAgg{}
		for i := 1; i < len(ts); i++ {
			cur := ts[i]
			e, _ := att.entity(binds, spawn, cur.m)
			if e == "" || !want(e) {
				continue
			}
			f := touched[e]
			if f == nil {
				k := aggKey{e, sess, file}
				if agg[k] == nil {
					agg[k] = &fileAgg{}
				}
				f = agg[k]
				touched[e] = f
			}
			f.Timed = true
			gap := cur.m.ts.Sub(ts[i-1].m.ts)
			if cur.typ == tickPrompt {
				if !sub {
					f.WaitSec += int64(gap.Round(time.Second) / time.Second)
				}
				continue
			}
			gap = min(gap, maxPause).Round(time.Second)
			if gap <= 0 {
				continue
			}
			if f.Time == nil {
				f.Time = map[string]int64{}
			}
			f.Time[att.phase(e, cur.m.ts)+timeKeySplit+cur.activity] += int64(gap / time.Second)
			if !sub {
				f.Spans = append(f.Spans, [2]time.Time{cur.m.ts.Add(-gap), cur.m.ts})
			}
		}
		for _, f := range touched {
			f.Spans = unite(f.Spans)
		}
	}
}

// unite merges overlapping or touching intervals, sorted.
func unite(spans [][2]time.Time) [][2]time.Time {
	if len(spans) < 2 {
		return spans
	}
	slices.SortFunc(spans, func(a, b [2]time.Time) int { return a[0].Compare(b[0]) })
	out := spans[:1]
	for _, s := range spans[1:] {
		last := &out[len(out)-1]
		if !s[0].After(last[1]) {
			if s[1].After(last[1]) {
				last[1] = s[1]
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

// foldTiming adds one file's measured time to s: main-thread files into the
// active total, by phase and by activity, and the intervals the wall clock is
// taken from; subagent files into the sidechain block.
func (s *share) foldTiming(file string, f *fileAgg) {
	if !f.Timed {
		return
	}
	if s.Timing == nil {
		s.Timing = &Timing{ByPhase: map[string]int64{}, ByActivity: map[string]int64{}}
	}
	t := s.Timing
	sub := strings.Contains(file, "/subagents/")
	for key, sec := range f.Time {
		phase, act, _ := strings.Cut(key, timeKeySplit)
		if sub {
			if t.Sidechain == nil {
				t.Sidechain = &SidechainTime{ByActivity: map[string]int64{}}
			}
			t.Sidechain.ActiveSeconds += sec
			t.Sidechain.ByActivity[act] += sec
			continue
		}
		t.ActiveSeconds += sec
		t.ByPhase[phase] += sec
		t.ByActivity[act] += sec
	}
	t.HumanWaitSeconds += f.WaitSec
	t.spans = append(t.spans, f.Spans...)
}

// finish sets the wall-clock time: the length of the union of the intervals.
func (t *Timing) finish() {
	t.spans = unite(t.spans)
	var wall time.Duration
	for _, sp := range t.spans {
		wall += sp[1].Sub(sp[0])
	}
	t.WallSeconds = int64(wall / time.Second)
}

// merge adds o to t and returns the result, a copy: t and o stay as they were.
// The wall time of a merge is the sum — meaningful for one entity (merged
// once), and the project view's total is a plain sum too. nil merges as
// "nothing measured": nil only when both are.
func (t *Timing) merge(o *Timing) *Timing {
	if o == nil {
		return t
	}
	out := &Timing{ByPhase: map[string]int64{}, ByActivity: map[string]int64{}}
	for _, x := range []*Timing{t, o} {
		if x == nil {
			continue
		}
		out.ActiveSeconds += x.ActiveSeconds
		out.WallSeconds += x.WallSeconds
		out.HumanWaitSeconds += x.HumanWaitSeconds
		for k, v := range x.ByPhase {
			out.ByPhase[k] += v
		}
		for k, v := range x.ByActivity {
			out.ByActivity[k] += v
		}
		if x.Sidechain != nil {
			if out.Sidechain == nil {
				out.Sidechain = &SidechainTime{ByActivity: map[string]int64{}}
			}
			out.Sidechain.ActiveSeconds += x.Sidechain.ActiveSeconds
			for k, v := range x.Sidechain.ByActivity {
				out.Sidechain.ByActivity[k] += v
			}
		}
	}
	return out
}
