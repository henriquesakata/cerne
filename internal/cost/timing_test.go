package cost

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func lifeEvent(t *testing.T, repo, id, typ string, ts time.Time) {
	t.Helper()
	if err := store.AppendEvent(repo, domain.Event{ID: typ + id + ts.String(), At: ts, EntityID: id, Type: typ, Actor: "agent"}); err != nil {
		t.Fatal(err)
	}
}

func asst(ts time.Time, id string, tools ...ttool) string {
	return tmsg{ts: ts, id: id, req: "r" + id, c: cc(1, 0, 0, 0), tools: tools}.line("s")
}

func tool(id, name string, input map[string]any) ttool {
	return ttool{id: id, name: name, input: input}
}

const mn = int64(60)

// timingOf computes the time of id for a session s bound to id from at(-5).
func timingOf(t *testing.T, in Inputs, id string) *Timing {
	t.Helper()
	r := compute(t, in, "")
	s := r.perEntity[id]
	if s == nil || s.Timing == nil {
		t.Fatalf("no timing for %s: %+v", id, s)
	}
	return s.Timing
}

func timingFixture(t *testing.T, lines ...string) Inputs {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	writeLines(t, in.Roots[0], "-tmp-p/s.jsonl", lines...)
	turnBind(t, in.CacheDir, "s", "F-0001", at(-5), false)
	return in
}

// TestTimingGapGoesToTheClosingLine (F-0528 crit. 1, 2, 8): the pause up to an
// assistant line counts on that line's activity, up to a tool_result on the
// activity of the call it answers (two parallel tools included), up to a human
// prompt only as wait.
func TestTimingGapGoesToTheClosingLine(t *testing.T) { // F-0528 crit. 1, 2, 8
	read := tool("t1", "Read", map[string]any{"file_path": "a.go"})
	test := tool("t2", "Bash", map[string]any{"command": "go test ./..."})
	in := timingFixture(t,
		userLine("s", at(0), "go", nil),
		asst(at(1), "m1", read, test),                                                // 1 mn of model: pesquisa
		userLine("s", at(4), resultContent("t2"), nil),                               // 3 mn: the go test tool
		userLine("s", at(5), resultContent("t1"), nil),                               // 1 mn: the read
		asst(at(7), "m2"),                                                            // 2 mn: resposta
		userLine("s", at(17), "again", nil),                                          // 10 mn: wait
		asst(at(18), "m3", tool("t3", "Write", map[string]any{"file_path": "x.go"})), // 1 mn: código
	)
	tm := timingOf(t, in, "F-0001")
	want := map[string]int64{actResearch: 2 * mn, actTest: 3 * mn, actReply: 2 * mn, actCode: 1 * mn}
	if tm.ActiveSeconds != 8*mn || !reflect.DeepEqual(tm.ByActivity, want) {
		t.Fatalf("active %d by activity %v, want %d %v", tm.ActiveSeconds, tm.ByActivity, 8*mn, want)
	}
	if tm.HumanWaitSeconds != 10*mn {
		t.Fatalf("human wait = %d, want %d", tm.HumanWaitSeconds, 10*mn)
	}
}

// TestTimingCapsLongPause (F-0528 crit. 1): a 50-minute pause up to a
// tool_result counts 30; the one up to a prompt is whole in the wait.
func TestTimingCapsLongPause(t *testing.T) { // F-0528 crit. 1
	in := timingFixture(t,
		userLine("s", at(0), "go", nil),
		asst(at(1), "m1", tool("t1", "Read", map[string]any{"file_path": "a"})),
		userLine("s", at(51), resultContent("t1"), nil),
		userLine("s", at(200), "back", nil),
	)
	tm := timingOf(t, in, "F-0001")
	if tm.ActiveSeconds != 31*mn || tm.HumanWaitSeconds != 149*mn {
		t.Fatalf("active %d wait %d, want %d and %d", tm.ActiveSeconds, tm.HumanWaitSeconds, 31*mn, 149*mn)
	}
}

// TestTimingSplitsByPhase (F-0528 crit. 4, 5): the phase is the one in force at
// the line that closes the pause, from the event log; an entity without
// lifecycle events is all sem fase; both splits sum to the active time.
func TestTimingSplitsByPhase(t *testing.T) { // F-0528 crit. 4, 5
	in := timingFixture(t,
		userLine("s", at(0), "go", nil),
		asst(at(1), "m1"),  // spec
		asst(at(11), "m2"), // plano (approved at 10)
		asst(at(21), "m3"), // implementação (started at 20)
		asst(at(31), "m4"), // revisão (completed at 30)
	)
	lifeEvent(t, in.RepoDir, "F-0001", "feature.created", at(-5))
	lifeEvent(t, in.RepoDir, "F-0001", "feature.approved", at(10))
	lifeEvent(t, in.RepoDir, "F-0001", "feature.started", at(20))
	lifeEvent(t, in.RepoDir, "F-0001", "feature.completed", at(30))
	// A binding closes at its entity's completion: review time arrives through
	// a later bind.
	turnBind(t, in.CacheDir, "s", "F-0001", at(30).Add(time.Second), false)
	tm := timingOf(t, in, "F-0001")
	want := map[string]int64{phaseSpec: 1 * mn, phasePlan: 10 * mn, phaseDev: 10 * mn, phaseReview: 10 * mn}
	if !reflect.DeepEqual(tm.ByPhase, want) {
		t.Fatalf("by phase = %v, want %v", tm.ByPhase, want)
	}
	var byAct int64
	for _, v := range tm.ByActivity {
		byAct += v
	}
	if tm.ActiveSeconds != 31*mn || byAct != tm.ActiveSeconds {
		t.Fatalf("active %d, by activity %d: want both 31 mn", tm.ActiveSeconds, byAct)
	}

	in = timingFixture(t, userLine("s", at(0), "go", nil), asst(at(2), "m1"))
	if tm := timingOf(t, in, "F-0001"); !reflect.DeepEqual(tm.ByPhase, map[string]int64{phaseNone: 2 * mn}) {
		t.Fatalf("no events: by phase = %v, want all sem fase", tm.ByPhase)
	}
}

// TestTimingIgnoresPauseOutsideBinding (F-0528 crit. 1): a pause whose closing
// line falls before any binding counts for no entity.
func TestTimingIgnoresPauseOutsideBinding(t *testing.T) { // F-0528 crit. 1
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	writeLines(t, in.Roots[0], "-tmp-p/s.jsonl",
		userLine("s", at(0), "go", nil),
		asst(at(3), "m1"), // before the bind: nobody's
		asst(at(5), "m2"),
		asst(at(9), "m3"))
	turnBind(t, in.CacheDir, "s", "F-0001", at(4), false)
	tm := timingOf(t, in, "F-0001")
	if tm.ActiveSeconds != 6*mn { // m2 (2 mn) + m3 (4 mn)
		t.Fatalf("active = %d, want %d", tm.ActiveSeconds, 6*mn)
	}
}

// TestTimingSubagentIsSidechain (F-0528 crit. 7): a subagent file's time is the
// sidechain block, by activity, and never in the main thread's.
func TestTimingSubagentIsSidechain(t *testing.T) { // F-0528 crit. 7
	in := timingFixture(t,
		userLine("s", at(0), "go", nil),
		asst(at(1), "m1"))
	writeLines(t, in.Roots[0], "-tmp-p/s/subagents/agent-x.jsonl",
		tmsg{ts: at(2), id: "x1", req: "rx1", agent: "x", c: cc(1, 0, 0, 0)}.line("s"),
		tmsg{ts: at(7), id: "x2", req: "rx2", agent: "x", c: cc(1, 0, 0, 0), tools: []ttool{tool("u1", "Grep", nil)}}.line("s"))
	tm := timingOf(t, in, "F-0001")
	if tm.ActiveSeconds != 1*mn {
		t.Fatalf("main active = %d, want 60", tm.ActiveSeconds)
	}
	if tm.Sidechain == nil || tm.Sidechain.ActiveSeconds != 5*mn || tm.Sidechain.ByActivity[actResearch] != 5*mn {
		t.Fatalf("sidechain = %+v, want 5 mn of pesquisa", tm.Sidechain)
	}
}

// TestTimingReportAddsUp (F-0528 crit. 5, 10): the entity report carries the
// time fields of crit. 10 in seconds, and the sum by phase and the sum by
// activity each equal the active time.
func TestTimingReportAddsUp(t *testing.T) { // F-0528 crit. 5, 10
	in := timingFixture(t,
		userLine("s", at(0), "go", nil),
		asst(at(1), "m1", tool("t1", "Read", map[string]any{"file_path": "a"})),
		userLine("s", at(3), resultContent("t1"), nil),
		asst(at(6), "m2", tool("t2", "Write", map[string]any{"file_path": "x.go"})))
	lifeEvent(t, in.RepoDir, "F-0001", "feature.approved", at(2))
	r := compute(t, in, "F-0001")
	if r.Timing == nil || r.ActiveSeconds != 6*mn {
		t.Fatalf("report timing = %+v, want 360 s active", r.Timing)
	}
	for name, m := range map[string]map[string]int64{"phase": r.ByPhase, "activity": r.ByActivity} {
		var sum int64
		for _, v := range m {
			sum += v
		}
		if sum != r.ActiveSeconds {
			t.Errorf("sum by %s = %d, want %d", name, sum, r.ActiveSeconds)
		}
	}
	raw, _ := json.Marshal(r)
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"active_seconds", "wall_seconds", "human_wait_seconds", "by_phase", "by_activity"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("--json lacks %q: %s", k, raw)
		}
	}
}

// TestTimingWallIsTheUnionOfSessions (F-0528 crit. 6): two main sessions
// working at once add up in active time and overlap in wall time.
func TestTimingWallIsTheUnionOfSessions(t *testing.T) { // F-0528 crit. 6
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	for sess, mins := range map[string][2]int{"s": {0, 10}, "u": {5, 15}} {
		writeLines(t, in.Roots[0], "-tmp-p/"+sess+".jsonl",
			userLine(sess, at(mins[0]), "go", nil),
			tmsg{ts: at(mins[1]), id: "m" + sess, req: "r" + sess, c: cc(1, 0, 0, 0)}.line(sess))
		turnBind(t, in.CacheDir, sess, "F-0001", at(-5), false)
	}
	r := compute(t, in, "F-0001")
	if r.ActiveSeconds != 20*mn || r.WallSeconds != 15*mn {
		t.Fatalf("active %d wall %d, want %d and %d", r.ActiveSeconds, r.WallSeconds, 20*mn, 15*mn)
	}
}

// TestTimingSurvivesAnchorAndSnapshot (F-0528 crit. 12, 13): the stamp carries
// the computed time; a pruned session's time comes back from its snapshot; an
// anchor or snapshot from before the feature reads as not measured, never zero.
func TestTimingSurvivesAnchorAndSnapshot(t *testing.T) { // F-0528 crit. 12, 13
	in := timingFixture(t,
		userLine("s", at(0), "go", nil),
		asst(at(4), "m1"))
	live := compute(t, in, "F-0001")
	harvest(t, in, "s", sessDir(in, "s.jsonl"), time.Now())
	prune(t, in, "s")
	rec := compute(t, in, "F-0001")
	if rec.Timing == nil || rec.ActiveSeconds != 4*mn || rec.WallSeconds != 4*mn {
		t.Fatalf("recovered from the snapshot: %+v, want 240 s", rec.Timing)
	}
	saveFeature(t, in.RepoDir, "F-0001", domain.StateImplemented)
	writeResult(t, in.RepoDir, "F-0001", mustStamp(t, resultDoc("F-0001"), live))
	if err := DropSnapshot(in.CacheDir, "F-0001"); err != nil {
		t.Fatal(err)
	}
	if st, ok := StampedReport(in.RepoDir, "F-0001"); !ok || st.Timing == nil || st.ActiveSeconds != 4*mn || st.WallSeconds != 4*mn {
		t.Fatalf("stamped report timing = %+v, want the stamped 240 s", st.Timing)
	}

	// Before the feature: an anchor with no time, and a snapshot with no Timed.
	old := stampReport("F-0002", cc(1, 0, 0, 0), 1)
	writeResult(t, in.RepoDir, "F-0002", mustStamp(t, resultDoc("F-0002"), old))
	if st, ok := StampedReport(in.RepoDir, "F-0002"); !ok || st.Timing != nil {
		t.Fatalf("old anchor: timing = %+v, want nil (not measured)", st.Timing)
	}
	saveFeature(t, in.RepoDir, "F-0003", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "o", tmsg{ts: at(2), id: "a", req: "r", c: cc(1, 0, 0, 0)})
	bindAt(t, in.CacheDir, "o", "", "F-0003", at(1))
	harvest(t, in, "o", "", time.Now())
	snap := readSnap(t, in.CacheDir, "F-0003")
	for _, f := range snap.Sessions["o"].Files {
		f.Timed, f.Time, f.WaitMs, f.Spans = false, nil, 0, nil
	}
	if err := snap.save(in.CacheDir); err != nil {
		t.Fatal(err)
	}
	prune(t, in, "o")
	if r := compute(t, in, "F-0003"); r.Messages != 1 || r.Timing != nil {
		t.Fatalf("old snapshot: messages %d timing %+v, want 1 and nil", r.Messages, r.Timing)
	}
}

// TestTimingWakeUpIsWait (F-0528 crit. 1, 8): a session woken by another
// session's message (a meta user line) and the person's answer to
// AskUserQuestion are waiting, never active time — capped at 30 minutes each,
// they used to inflate the activity of the line after them.
func TestTimingWakeUpIsWait(t *testing.T) { // F-0528 crit. 1, 8
	ask := tool("t1", "AskUserQuestion", map[string]any{})
	in := timingFixture(t,
		userLine("s", at(0), "go", nil),
		asst(at(1), "m1"), // 1 mn: resposta
		userLine("s", at(121), "Another Claude session sent a message", map[string]any{"isMeta": true}), // 120 mn: wait
		asst(at(122), "m2", ask),                                                  // 1 mn: the model writing the question
		userLine("s", at(162), resultContent("t1"), nil),                          // 40 mn: the person answering
		asst(at(163), "m3", tool("t2", "Read", map[string]any{"file_path": "a"})), // 1 mn: pesquisa
	)
	tm := timingOf(t, in, "F-0001")
	want := map[string]int64{actReply: 2 * mn, actResearch: 1 * mn}
	if tm.ActiveSeconds != 3*mn || !reflect.DeepEqual(tm.ByActivity, want) {
		t.Fatalf("active %d by activity %v, want %d %v", tm.ActiveSeconds, tm.ByActivity, 3*mn, want)
	}
	if tm.HumanWaitSeconds != 160*mn {
		t.Fatalf("wait = %d, want %d", tm.HumanWaitSeconds, 160*mn)
	}
}

// TestTimingReplyGenerationIsActive (F-0528 crit. 1, 2): a streamed message
// ends at its last line, so a long reply's generation is active time, not
// wait (critic, round 1).
func TestTimingReplyGenerationIsActive(t *testing.T) { // F-0528 crit. 1, 2
	in := timingFixture(t,
		userLine("s", at(0), "go", nil),
		tmsg{ts: at(1), id: "m1", req: "r1", c: cc(1, 0, 0, 1)}.line("s"),
		tmsg{ts: at(4), id: "m1", req: "r1", c: cc(1, 0, 0, 1)}.line("s"), // the same message, its last block
		userLine("s", at(10), "next", nil),
	)
	tm := timingOf(t, in, "F-0001")
	if tm.ActiveSeconds != 4*mn || tm.HumanWaitSeconds != 6*mn {
		t.Fatalf("active %d wait %d, want %d and %d", tm.ActiveSeconds, tm.HumanWaitSeconds, 4*mn, 6*mn)
	}
}

// TestTimingSkipsLineWithoutTimestamp (F-0528 crit. 1): a line whose
// timestamp does not parse adds no tick: the pause after it would start at
// year 1 (critic, round 1).
func TestTimingSkipsLineWithoutTimestamp(t *testing.T) { // F-0528 crit. 1
	bad := `{"type":"assistant","timestamp":"not a time","sessionId":"s","requestId":"rx","message":{"id":"mx","model":"claude-opus-5-5","usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":1}}}`
	in := timingFixture(t,
		userLine("s", at(0), "go", nil),
		bad,
		asst(at(2), "m1"),
		userLine("s", at(10), "next", nil),
	)
	tm := timingOf(t, in, "F-0001")
	if tm.ActiveSeconds != 2*mn || tm.HumanWaitSeconds != 8*mn {
		t.Fatalf("active %d wait %d, want %d and %d", tm.ActiveSeconds, tm.HumanWaitSeconds, 2*mn, 8*mn)
	}
}

// TestTimingPhaseFollowsRevert (F-0528 crit. 4): the phase replays the
// lifecycle up to the line — a revert from implemented puts the feature back
// in implementação, not in revisão for good (critic, round 1).
func TestTimingPhaseFollowsRevert(t *testing.T) { // F-0528 crit. 4
	in := timingFixture(t,
		userLine("s", at(0), "go", nil),
		userLine("s", at(16), "again", nil),
		asst(at(20), "m1"),
	)
	lifeEvent(t, in.RepoDir, "F-0001", "feature.started", at(-3))
	lifeEvent(t, in.RepoDir, "F-0001", "feature.completed", at(5))
	lifeEvent(t, in.RepoDir, "F-0001", "feature.reverted", at(15))
	turnBind(t, in.CacheDir, "s", "F-0001", at(15).Add(time.Second), false)
	tm := timingOf(t, in, "F-0001")
	if want := map[string]int64{phaseDev: 4 * mn}; !reflect.DeepEqual(tm.ByPhase, want) {
		t.Fatalf("by phase = %v, want %v", tm.ByPhase, want)
	}
}

// TestTimingRoundsOncePerKey (F-0528 crit. 1, 5, 6): pauses add up in
// milliseconds and round once per phase/activity: ten pauses of 0.6 s are 6 s,
// not 10, and a single session's wall clock never falls below its active time
// (Gauntlet round 1: 81 s active over 79 s of wall).
func TestTimingRoundsOncePerKey(t *testing.T) { // F-0528 crit. 1, 5, 6
	lines := []string{userLine("s", at(0), "go", nil)}
	t0 := at(0)
	for i := 1; i <= 10; i += 2 {
		id := fmt.Sprintf("t%d", i)
		lines = append(lines,
			asst(t0.Add(time.Duration(i)*600*time.Millisecond), "m"+id, tool(id, "Read", map[string]any{"file_path": "a"})),
			userLine("s", t0.Add(time.Duration(i+1)*600*time.Millisecond), resultContent(id), nil))
	}
	tm := timingOf(t, timingFixture(t, lines...), "F-0001")
	if tm.ActiveSeconds != 6 || tm.WallSeconds != 6 || tm.ByActivity[actResearch] != 6 {
		t.Fatalf("active %d wall %d research %d, want 6, 6, 6", tm.ActiveSeconds, tm.WallSeconds, tm.ByActivity[actResearch])
	}
}

// TestTimingWallNeverAboveActive (F-0528 crit. 6): the union of the
// intervals never exceeds their sum; per-key rounding must not make it look
// so (Gauntlet round 2: 81 s of wall over 80 s active in one session).
func TestTimingWallNeverAboveActive(t *testing.T) { // F-0528 crit. 6
	t0 := at(0)
	in := timingFixture(t,
		userLine("s", t0, "go", nil),
		asst(t0.Add(400*time.Millisecond), "m1", tool("t1", "Read", map[string]any{"file_path": "a"})), // pesquisa 0.4 s
		userLine("s", t0.Add(400*time.Millisecond), resultContent("t1"), nil),
		asst(t0.Add(800*time.Millisecond), "m2", tool("t2", "Write", map[string]any{"file_path": "x.go"})), // código 0.4 s
		userLine("s", t0.Add(800*time.Millisecond), resultContent("t2"), nil),
	) // each key rounds to 0, the union of 0.8 s to 1
	tm := timingOf(t, in, "F-0001")
	if tm.WallSeconds > tm.ActiveSeconds {
		t.Fatalf("wall %d above active %d", tm.WallSeconds, tm.ActiveSeconds)
	}
}
