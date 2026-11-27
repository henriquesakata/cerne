package cost

import (
	"fmt"
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

// lifeEvents move a feature between phases; each maps the phase it leaves
// the feature in. feature.reverted walks one step back (afterRevert).
var lifeEvents = map[string]string{
	"feature.created": phaseSpec, "feature.approved": phasePlan, "feature.started": phaseDev,
	"feature.completed": phaseReview, "feature.accepted": phaseReview, "feature.reverted": "",
}

// afterRevert is the phase a revert leaves: approved → candidate, implemented
// → in_development, done → implemented.
var afterRevert = map[string]string{phasePlan: phaseSpec, phaseReview: phaseDev}

// lifeMove is one lifecycle move of an entity.
type lifeMove struct {
	at  time.Time
	typ string
}

// Timing is the measured working time of one entity, in seconds (F-0528):
// the pauses between the lines of its main-thread sessions, split by lifecycle
// phase and by activity, apart from the wait for a prompt (a person, another
// session, a task notice) and from the
// subagents' time. nil means not measured, never zero.
type Timing struct {
	ActiveSeconds    int64            `json:"active_seconds"`
	WallSeconds      int64            `json:"wall_seconds"`
	HumanWaitSeconds int64            `json:"human_wait_seconds"`
	ByPhase          map[string]int64 `json:"by_phase"`
	ByActivity       map[string]int64 `json:"by_activity"`
	Sidechain        *SidechainTime   `json:"sidechain_time"` // null without subagents: always present (crit. 10)
	spans            [][2]time.Time   // the main-thread active intervals, united; not serialized
	ms, sideMs       map[string]int64 // folded milliseconds (phase/activity, activity), rounded once in finish
	waitMs           int64
}

// SidechainTime is the subagents' active time, measured by the same rules and
// kept out of the main thread's.
type SidechainTime struct {
	ActiveSeconds int64            `json:"active_seconds"`
	ByActivity    map[string]int64 `json:"by_activity"`
}

// phase is the lifecycle phase of entity at t, replaying its events up to t
// (a revert walks back, a reopen counts again); sem fase when the entity has
// none (a W- without a feature, an old backend).
func (a attributor) phase(entity string, t time.Time) string {
	evs := a.life[entity]
	if len(evs) == 0 {
		return phaseNone
	}
	ph, done := phaseSpec, false // done: accepted once, so a revert lands on implemented
	for _, ev := range evs {
		if ev.at.After(t) {
			break
		}
		switch {
		case ev.typ == "feature.reverted" && done:
			done = false // done → implemented: still revisão
		case ev.typ == "feature.reverted":
			if back, ok := afterRevert[ph]; ok {
				ph = back
			}
		default:
			ph, done = lifeEvents[ev.typ], ev.typ == "feature.accepted"
		}
	}
	return ph
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
			if waiting(cur) {
				if !sub {
					f.WaitMs += gap.Milliseconds()
				}
				continue
			}
			gap = min(gap, maxPause)
			if gap <= 0 {
				continue
			}
			if f.Time == nil {
				f.Time = map[string]int64{}
			}
			act := cur.activity
			if act == actWait { // the assistant line asking the question: the model wrote it
				act = actReply
			}
			f.Time[att.phase(e, cur.m.ts)+timeKeySplit+act] += gap.Milliseconds()
			if !sub {
				f.Spans = append(f.Spans, [2]time.Time{cur.m.ts.Add(-gap), cur.m.ts})
			}
		}
		for _, f := range touched {
			f.Spans = unite(f.Spans)
		}
	}
}

// waiting reports whether the pause up to cur is the session waiting, not
// working: it ends at a prompt (a person, another session, a task
// notification) or at the person's answer to a question.
func waiting(cur tick) bool {
	return cur.typ == tickPrompt || cur.typ == tickResult && cur.activity == actWait
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

// foldTiming adds one file's measured time to s, in milliseconds: main-thread
// files by phase and activity, with the intervals the wall clock is taken
// from; subagent files into the sidechain. finish turns it into seconds.
func (s *share) foldTiming(file string, f *fileAgg) {
	if !f.Timed {
		return
	}
	if s.Timing == nil {
		s.Timing = &Timing{ByPhase: map[string]int64{}, ByActivity: map[string]int64{}, ms: map[string]int64{}, sideMs: map[string]int64{}}
	}
	t := s.Timing
	sub := strings.Contains(file, "/subagents/")
	for key, ms := range f.Time {
		if sub {
			_, act, _ := strings.Cut(key, timeKeySplit)
			t.sideMs[act] += ms
			continue
		}
		t.ms[key] += ms
	}
	t.waitMs += f.WaitMs
	t.spans = append(t.spans, f.Spans...)
}

// finish turns the folded milliseconds into seconds, rounding once per key so
// that the phases and the activities each sum to the active total, and sets
// the wall clock: the length of the union of the intervals.
func (t *Timing) finish() {
	if t.ms == nil { // read from an anchor, or finished already
		return
	}
	sec := func(ms int64) int64 { return (ms + 500) / 1000 }
	for key, ms := range t.ms {
		phase, act, _ := strings.Cut(key, timeKeySplit)
		s := sec(ms)
		t.ActiveSeconds += s
		t.ByPhase[phase] += s
		t.ByActivity[act] += s
	}
	for act, ms := range t.sideMs {
		if t.Sidechain == nil {
			t.Sidechain = &SidechainTime{ByActivity: map[string]int64{}}
		}
		s := sec(ms)
		t.Sidechain.ActiveSeconds += s
		t.Sidechain.ByActivity[act] += s
	}
	t.HumanWaitSeconds = sec(t.waitMs)
	t.spans = unite(t.spans)
	var wall time.Duration
	for _, sp := range t.spans {
		wall += sp[1].Sub(sp[0])
	}
	t.WallSeconds = int64(wall.Round(time.Second) / time.Second)
	t.ms, t.sideMs, t.waitMs = nil, nil, 0
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

// FormatDuration writes sec as "1h 12min", "45min" or "30s": the one form the
// CLI and the stamp share.
func FormatDuration(sec int64) string {
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	m := (sec + 30) / 60
	switch {
	case m < 60:
		return fmt.Sprintf("%dmin", m)
	case m%60 == 0:
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dh %dmin", m/60, m%60)
}

// PhaseOrder is the phases in lifecycle order, for display.
var PhaseOrder = []string{phaseSpec, phasePlan, phaseDev, phaseReview, phaseNone}

// Breakdown lists m as "key time" parts, phases in lifecycle order when order
// names them, else the larger first; ties by key.
func Breakdown(m map[string]int64, order []string) []string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v > 0 {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b string) int {
		if order != nil {
			return slices.Index(order, a) - slices.Index(order, b)
		}
		if m[a] != m[b] {
			return int(m[b] - m[a])
		}
		return strings.Compare(a, b)
	})
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + " " + FormatDuration(m[k])
	}
	return parts
}
