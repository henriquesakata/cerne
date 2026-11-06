package cost

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

var base = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

// at is base plus n minutes.
func at(n int) time.Time { return base.Add(time.Duration(n) * time.Minute) }

func cc(in, cw, cr, out int64) Counters {
	return Counters{Input: in, CacheCreation: cw, CacheRead: cr, Output: out}
}

// tmsg is one synthetic assistant transcript line.
type tmsg struct {
	ts      time.Time
	id, req string
	model   string
	agent   string
	from    string // sessionId stamped on the line when not the file's (a resume copy)
	bare    bool   // no sessionId on the line at all
	c       Counters
}

func (m tmsg) line(sess string) string {
	if m.from != "" {
		sess = m.from
	}
	model := m.model
	if model == "" {
		model = "claude-opus-5-5"
	}
	o := map[string]any{
		"type": "assistant", "timestamp": m.ts.Format(time.RFC3339Nano), "sessionId": sess,
		"message": map[string]any{"id": m.id, "model": model, "usage": map[string]any{
			"input_tokens": m.c.Input, "cache_creation_input_tokens": m.c.CacheCreation,
			"cache_read_input_tokens": m.c.CacheRead, "output_tokens": m.c.Output,
		}},
	}
	if m.req != "" {
		o["requestId"] = m.req
	}
	if m.bare {
		delete(o, "sessionId")
	}
	if m.agent != "" {
		o["isSidechain"], o["agentId"] = true, m.agent
	}
	raw, _ := json.Marshal(o)
	return string(raw)
}

// writeLines writes raw JSONL lines to <root>/projects/<rel>.
func writeLines(t *testing.T, root, rel string, lines ...string) string {
	t.Helper()
	p := filepath.Join(root, "projects", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeMain writes sess's main transcript (agent "") or the subagent's file.
func writeSession(t *testing.T, root, sess string, msgs ...tmsg) {
	t.Helper()
	byFile := map[string][]string{}
	for _, m := range msgs {
		rel := "-tmp-p/" + sess + ".jsonl"
		if m.agent != "" {
			rel = "-tmp-p/" + sess + "/subagents/agent-" + m.agent + ".jsonl"
		}
		byFile[rel] = append(byFile[rel], m.line(sess))
	}
	for rel, lines := range byFile {
		writeLines(t, root, rel, lines...)
	}
}

func bindAt(t *testing.T, cache, sess, agent, entity string, ts time.Time) {
	t.Helper()
	if err := AppendBind(cache, Bind{Session: sess, Agent: agent, Entity: entity, Harness: HarnessClaudeCode, TS: ts}); err != nil {
		t.Fatal(err)
	}
}

func saveFeature(t *testing.T, repo, id string, state domain.FeatureState) {
	t.Helper()
	if err := store.SaveFeature(repo, store.Feature{ID: id, Title: "Feature " + id, State: state, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatal(err)
	}
}

// fixture is a cache, a backend clone and a transcript root, all temporary.
func fixture(t *testing.T) Inputs {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	return Inputs{CacheDir: t.TempDir(), RepoDir: t.TempDir(), Roots: []string{root}}
}

func compute(t *testing.T, in Inputs, id string) Report {
	t.Helper()
	r, err := Compute(in, id)
	if err != nil {
		t.Fatalf("Compute(%q): %v", id, err)
	}
	return r
}

// TestReadSessionDedupKeepsMaxPerCounter (crit. 3, no double count): the
// streamed chunks of one message (same message.id + requestId) count once,
// with the max per counter (the last chunk carries the real output) and the
// earliest ts; a line without requestId counts on its own; <synthetic> and
// non-assistant lines carrying "usage" are ignored.
func TestReadSessionDedupKeepsMaxPerCounter(t *testing.T) {
	root := t.TempDir()
	p := writeLines(t, root, "-tmp-p/s.jsonl",
		tmsg{ts: at(1), id: "m1", req: "r1", c: cc(3, 5, 7, 1)}.line("s"),
		tmsg{ts: at(2), id: "m1", req: "r1", c: cc(3, 5, 7, 1)}.line("s"),
		tmsg{ts: at(3), id: "m1", req: "r1", c: cc(3, 5, 7, 307)}.line("s"),
		tmsg{ts: at(4), id: "m2", c: cc(1, 0, 0, 2)}.line("s"),
		tmsg{ts: at(5), id: "m3", req: "r3", model: "<synthetic>", c: cc(100, 100, 100, 100)}.line("s"),
		`{"type":"user","timestamp":"2026-11-01T10:06:00Z","toolUseResult":{"usage":{"input_tokens":999,"output_tokens":999}},"message":{"role":"user","content":"x"}}`,
	)
	msgs, err := readSession("s", []tfile{{rel: "-tmp-p/s.jsonl", abs: p}})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2: %+v", len(msgs), msgs)
	}
	if msgs[0].c != cc(3, 5, 7, 307) || !msgs[0].ts.Equal(at(1)) {
		t.Fatalf("m1 = %+v at %v, want output 307 at the first chunk's ts", msgs[0].c, msgs[0].ts)
	}
	if msgs[1].c != cc(1, 0, 0, 2) {
		t.Fatalf("m2 = %+v", msgs[1].c)
	}
}

// TestAttributeRebindMidSession (crit. 3): a rebind A→B mid-session sends each
// message to the binding in force at its own timestamp; nothing is counted
// twice or dropped.
func TestAttributeRebindMidSession(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	saveFeature(t, in.RepoDir, "F-0002", domain.StateInDevelopment)
	c1, c3, c5 := cc(1, 10, 100, 1000), cc(2, 20, 200, 2000), cc(4, 40, 400, 4000)
	writeSession(t, in.Roots[0], "s",
		tmsg{ts: at(1), id: "m1", req: "r1", c: c1},
		tmsg{ts: at(3), id: "m3", req: "r3", c: c3},
		tmsg{ts: at(5), id: "m5", req: "r5", c: c5})
	bindAt(t, in.CacheDir, "s", "", "F-0001", at(2))
	bindAt(t, in.CacheDir, "s", "", "F-0002", at(4))

	r := compute(t, in, "")
	if r.Entities["F-0001"] != c3 || r.Entities["F-0002"] != c5 || r.Unattributed != c1 {
		t.Fatalf("entities=%+v unattributed=%+v, want F-0001=%v F-0002=%v unattributed=%v", r.Entities, r.Unattributed, c3, c5, c1)
	}
	if sum := r.Entities["F-0001"].add(r.Entities["F-0002"]).add(r.Unattributed); sum != c1.add(c3).add(c5) {
		t.Fatalf("sum %+v != messages %+v", sum, c1.add(c3).add(c5))
	}
	if a := compute(t, in, "F-0001"); a.Counters != c3 || a.Messages != 1 || a.Unattributed != c1 {
		t.Fatalf("F-0001 = %+v", a)
	}
}

// TestAttributeOrphanToUnattributed (crit. 4): messages before any binding of
// their session land in the unattributed bucket, shown in the entity report.
func TestAttributeOrphanToUnattributed(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "s",
		tmsg{ts: at(1), id: "m1", req: "r1", c: cc(1, 2, 3, 4)},
		tmsg{ts: at(2), id: "m2", req: "r2", c: cc(1, 2, 3, 4)})
	bindAt(t, in.CacheDir, "s", "", "F-0001", at(3))

	r := compute(t, in, "F-0001")
	if r.Counters != (Counters{}) || r.Unattributed != cc(2, 4, 6, 8) || r.Sessions != 1 {
		t.Fatalf("report = %+v, want zero counters, unattributed 2/4/6/8, 1 session", r)
	}
}

// TestAttributeSubagentOwnBinding: a subagent's message follows its own
// binding once it has one, and the main thread's before that; the main thread
// never follows a subagent's binding.
func TestAttributeSubagentOwnBinding(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	saveFeature(t, in.RepoDir, "F-0003", domain.StateInDevelopment)
	x3, x5, m6 := cc(1, 0, 0, 1), cc(0, 2, 0, 2), cc(0, 0, 4, 4)
	writeSession(t, in.Roots[0], "s",
		tmsg{ts: at(3), id: "x3", req: "r", agent: "x", c: x3},
		tmsg{ts: at(5), id: "x5", req: "r", agent: "x", c: x5},
		tmsg{ts: at(6), id: "m6", req: "r", c: m6})
	bindAt(t, in.CacheDir, "s", "", "F-0001", at(1))
	bindAt(t, in.CacheDir, "s", "x", "F-0003", at(4))

	r := compute(t, in, "")
	if r.Entities["F-0001"] != x3.add(m6) || r.Entities["F-0003"] != x5 {
		t.Fatalf("entities = %+v, want F-0001=%v F-0003=%v", r.Entities, x3.add(m6), x5)
	}
}

func closeAt(t *testing.T, repo, id, typ string, ts time.Time) {
	t.Helper()
	ev := domain.NewEvent(id, typ, nil, "agent")
	ev.At = ts
	if err := store.AppendEvent(repo, ev); err != nil {
		t.Fatal(err)
	}
}

// TestAttributeBindingEndsWhenEntityCloses (C1): a binding ends when its entity
// closes — the work done/abandoned, the feature leaving in_development — so a
// later message of the session is unattributed, not charged to the last
// entity. The critic's t1 scenario plus a feature start → complete.
func TestAttributeBindingEndsWhenEntityCloses(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0003", domain.StateInDevelopment)
	saveFeature(t, in.RepoDir, "F-0004", domain.StateImplemented)
	if err := store.SaveWork(in.RepoDir, store.Work{ID: "W-0005", Title: "w", State: domain.WorkDone, Refs: []string{"F-0003"}, CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatal(err)
	}
	writeSession(t, in.Roots[0], "s",
		tmsg{ts: at(1), id: "before", req: "r", c: cc(1, 0, 0, 0)},
		tmsg{ts: at(3), id: "during", req: "r", c: cc(10, 0, 0, 0)},
		tmsg{ts: at(6), id: "after", req: "r", c: cc(100, 0, 0, 0)})
	bindAt(t, in.CacheDir, "s", "", "W-0005", at(2))
	closeAt(t, in.RepoDir, "W-0005", "work.done", at(5))
	writeSession(t, in.Roots[0], "f",
		tmsg{ts: at(3), id: "dev", req: "r", c: cc(0, 7, 0, 0)},
		tmsg{ts: at(5), id: "done", req: "r", c: cc(0, 70, 0, 0)})
	bindAt(t, in.CacheDir, "f", "", "F-0004", at(2))
	closeAt(t, in.RepoDir, "F-0004", "feature.completed", at(4))

	if r := compute(t, in, "F-0003"); r.Counters != cc(10, 0, 0, 0) || r.Unattributed != cc(101, 0, 0, 0) {
		t.Fatalf("F-0003 = %+v / unattributed %+v, want 10 / 101", r.Counters, r.Unattributed)
	}
	if r := compute(t, in, "F-0004"); r.Counters != cc(0, 7, 0, 0) || r.Unattributed != cc(0, 70, 0, 0) {
		t.Fatalf("F-0004 = %+v / unattributed %+v, want 7 / 70", r.Counters, r.Unattributed)
	}
}

// TestAttributeSubagentKeepsSpawnBinding (C2): a background subagent keeps the
// main-thread binding in force at its first message when the main thread
// rebinds meanwhile; its own binding still wins once it has one.
func TestAttributeSubagentKeepsSpawnBinding(t *testing.T) {
	in := fixture(t)
	for _, id := range []string{"F-0001", "F-0002", "F-0003"} {
		saveFeature(t, in.RepoDir, id, domain.StateInDevelopment)
	}
	x2, x4, x5, m6, x8 := cc(1, 0, 0, 0), cc(2, 0, 0, 0), cc(4, 0, 0, 0), cc(0, 1, 0, 0), cc(0, 0, 1, 0)
	writeSession(t, in.Roots[0], "s",
		tmsg{ts: at(2), id: "x2", req: "r", agent: "x", c: x2},
		tmsg{ts: at(4), id: "x4", req: "r", agent: "x", c: x4},
		tmsg{ts: at(5), id: "x5", req: "r", agent: "x", c: x5},
		tmsg{ts: at(6), id: "m6", req: "r", c: m6},
		tmsg{ts: at(8), id: "x8", req: "r", agent: "x", c: x8})
	bindAt(t, in.CacheDir, "s", "", "F-0001", at(1))
	bindAt(t, in.CacheDir, "s", "", "F-0002", at(3))
	bindAt(t, in.CacheDir, "s", "x", "F-0003", at(7))

	r := compute(t, in, "")
	if r.Entities["F-0001"] != x2.add(x4).add(x5) || r.Entities["F-0002"] != m6 || r.Entities["F-0003"] != x8 {
		t.Fatalf("entities = %+v, want F-0001=%v F-0002=%v F-0003=%v", r.Entities, x2.add(x4).add(x5), m6, x8)
	}
}

// sharedCopyFixture: message m sits in two sessions with no sessionId on the
// lines (a copy cerne cannot tell apart). In "orig" it is attributed to F-0001;
// in "copy" — whose first binding is older, a subagent's — it is outside any
// binding.
func sharedCopyFixture(t *testing.T) (Inputs, Counters) {
	t.Helper()
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	saveFeature(t, in.RepoDir, "F-0309", domain.StateInDevelopment)
	m := tmsg{ts: at(3), id: "m", req: "r", bare: true, c: cc(1, 10, 100, 1000)}
	writeSession(t, in.Roots[0], "orig", m)
	writeSession(t, in.Roots[0], "copy", m, tmsg{ts: at(5), id: "own", req: "r", agent: "y", c: cc(0, 0, 0, 7)})
	bindAt(t, in.CacheDir, "copy", "y", "F-0309", at(1))
	bindAt(t, in.CacheDir, "orig", "", "F-0001", at(2))
	return in, m.c
}

// TestComputeEntityUnattributedExcludesAttributedElsewhere (C8): the entity
// view attributes and dedups across every ledger session like the project
// view, so a copy attributed to another entity is not in its unattributed.
func TestComputeEntityUnattributedExcludesAttributedElsewhere(t *testing.T) {
	in, _ := sharedCopyFixture(t)
	if r := compute(t, in, "F-0309"); r.Unattributed != (Counters{}) || r.Counters != cc(0, 0, 0, 7) {
		t.Fatalf("F-0309 = %+v, unattributed %+v; want only its own message, nothing unattributed", r.Counters, r.Unattributed)
	}
}

// TestComputeSharedCopyCountsOnceAttributed (C11, mutations M3 and M9): a
// message present in two sessions counts once, and on the copy that is
// attributed — even when the unattributed copy's session is read first.
func TestComputeSharedCopyCountsOnceAttributed(t *testing.T) {
	in, m := sharedCopyFixture(t)
	r := compute(t, in, "")
	if r.Entities["F-0001"] != m || r.Unattributed != (Counters{}) || r.Counters != m.add(cc(0, 0, 0, 7)) {
		t.Fatalf("project = entities %+v, unattributed %+v; want m once, on F-0001", r.Entities, r.Unattributed)
	}
}

// TestAttributeBoundaryBelongsToNewBinding (C11, mutation M2): a message at
// exactly a binding's ts already belongs to that binding.
func TestAttributeBoundaryBelongsToNewBinding(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	saveFeature(t, in.RepoDir, "F-0002", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "s",
		tmsg{ts: at(1), id: "a", req: "r", c: cc(1, 0, 0, 0)},
		tmsg{ts: at(3), id: "b", req: "r", c: cc(0, 1, 0, 0)})
	bindAt(t, in.CacheDir, "s", "", "F-0001", at(1))
	bindAt(t, in.CacheDir, "s", "", "F-0002", at(3))
	if r := compute(t, in, ""); r.Entities["F-0001"] != cc(1, 0, 0, 0) || r.Entities["F-0002"] != cc(0, 1, 0, 0) {
		t.Fatalf("entities = %+v, want each boundary message on the binding starting at its ts", r.Entities)
	}
}

// TestShareWindowWidens: the window is the earliest first and the latest last
// over everything folded in, whatever the fold order (shares come from map
// iteration).
func TestShareWindowWidens(t *testing.T) {
	s := share{First: "2026-11-01T10:05:00Z", Last: "2026-11-01T10:05:00Z"}
	s.widen("2026-11-01T10:01:00Z", "2026-11-01T10:09:00Z")
	s.widen("2026-11-01T10:03:00Z", "2026-11-01T10:04:00Z")
	if s.First != "2026-11-01T10:01:00Z" || s.Last != "2026-11-01T10:09:00Z" {
		t.Fatalf("window = %s → %s", s.First, s.Last)
	}
}

// TestComputeResolvesWorkToFeature: a W- with exactly one feature ref counts
// on that feature; a W- with none stays itself, visible in the project view.
func TestComputeResolvesWorkToFeature(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0002", domain.StateInDevelopment)
	for id, refs := range map[string][]string{"W-0001": {"F-0002"}, "W-0003": nil} {
		if err := store.SaveWork(in.RepoDir, store.Work{ID: id, Title: id, State: domain.WorkActive, Refs: refs, CreatedAt: base, UpdatedAt: base}); err != nil {
			t.Fatal(err)
		}
	}
	writeSession(t, in.Roots[0], "s1", tmsg{ts: at(2), id: "a", req: "r", c: cc(1, 1, 1, 1)})
	writeSession(t, in.Roots[0], "s2", tmsg{ts: at(2), id: "b", req: "r", c: cc(2, 2, 2, 2)})
	bindAt(t, in.CacheDir, "s1", "", "W-0001", at(1))
	bindAt(t, in.CacheDir, "s2", "", "W-0003", at(1))

	if r := compute(t, in, "F-0002"); r.Counters != cc(1, 1, 1, 1) || r.Sessions != 1 {
		t.Fatalf("F-0002 = %+v", r)
	}
	r := compute(t, in, "")
	if r.Entities["F-0002"] != cc(1, 1, 1, 1) || r.Entities["W-0003"] != cc(2, 2, 2, 2) {
		t.Fatalf("project entities = %+v", r.Entities)
	}
	if _, ok := r.Entities["W-0001"]; ok {
		t.Fatalf("W-0001 should resolve to F-0002: %+v", r.Entities)
	}
}

// TestComputeMissingTranscriptCounted (crit. 11): a ledger session whose
// transcript is gone is counted, never an error.
func TestComputeMissingTranscriptCounted(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "s1", tmsg{ts: at(2), id: "a", req: "r", c: cc(1, 1, 1, 1)})
	bindAt(t, in.CacheDir, "s1", "", "F-0001", at(1))
	bindAt(t, in.CacheDir, "s2", "", "F-0001", at(1))

	r := compute(t, in, "F-0001")
	if r.Sessions != 2 || r.SessionsWithoutTranscript != 1 || r.Counters != cc(1, 1, 1, 1) {
		t.Fatalf("report = %+v, want 2 sessions, 1 without transcript", r)
	}
}

// TestComputeSidechainIncludedAndSplit (crit. 12): subagent messages count in
// the entity's total and are reported apart as the sidechain share.
func TestComputeSidechainIncludedAndSplit(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	main, side := cc(1, 2, 3, 4), cc(10, 20, 30, 40)
	writeSession(t, in.Roots[0], "s",
		tmsg{ts: at(2), id: "a", req: "r", c: main},
		tmsg{ts: at(3), id: "b", req: "r", agent: "x", c: side})
	bindAt(t, in.CacheDir, "s", "", "F-0001", at(1))

	r := compute(t, in, "F-0001")
	if r.Counters != main.add(side) || r.Sidechain != side || r.Messages != 2 {
		t.Fatalf("report = %+v, want counters %v sidechain %v", r, main.add(side), side)
	}
	if r.First != at(2).Format(time.RFC3339) || r.Last != at(3).Format(time.RFC3339) {
		t.Fatalf("window = %s → %s", r.First, r.Last)
	}
}

// TestComputeRefusesNonClaudeHarness (crit. 13): a binding recorded under
// another harness refuses the whole scope, naming the supported harness, with
// no partial number.
func TestComputeRefusesNonClaudeHarness(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "s", tmsg{ts: at(2), id: "a", req: "r", c: cc(1, 1, 1, 1)})
	bindAt(t, in.CacheDir, "s", "", "F-0001", at(1))
	if err := AppendBind(in.CacheDir, Bind{Session: "s2", Entity: "F-0001", Harness: "other", TS: at(1)}); err != nil {
		t.Fatal(err)
	}

	r, err := Compute(in, "F-0001")
	if !errors.Is(err, ErrUnsupportedHarness) || !strings.Contains(err.Error(), "Claude Code") || !strings.Contains(err.Error(), "s2") {
		t.Fatalf("err = %v, want ErrUnsupportedHarness naming Claude Code and s2", err)
	}
	if r.Counters != (Counters{}) || r.Messages != 0 || r.Unattributed != (Counters{}) {
		t.Fatalf("partial report leaked: %+v", r)
	}
}

// TestComputeProjectViewExcludesOtherHarness (C9): the project view leaves out
// and names the sessions bound under another harness instead of failing; an
// entity scope holding one still refuses (crit. 13).
func TestComputeProjectViewExcludesOtherHarness(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "s", tmsg{ts: at(2), id: "a", req: "r", c: cc(1, 1, 1, 1)})
	bindAt(t, in.CacheDir, "s", "", "F-0001", at(1))
	if err := AppendBind(in.CacheDir, Bind{Session: "s2", Entity: "F-0002", Harness: "other", TS: at(1)}); err != nil {
		t.Fatal(err)
	}
	r := compute(t, in, "")
	if r.Entities["F-0001"] != cc(1, 1, 1, 1) || !reflect.DeepEqual(r.OtherHarness, []string{"s2"}) || r.Sessions != 1 {
		t.Fatalf("project = %+v, want F-0001 counted, s2 excluded and named", r)
	}
	if _, err := Compute(in, "F-0002"); !errors.Is(err, ErrUnsupportedHarness) {
		t.Fatalf("entity scope err = %v, want ErrUnsupportedHarness", err)
	}
}

// TestComputeNoLedgerIsZeroWithReason (crit. 14): no ledger is zero with the
// reason, not an error; a ledger with no line for the entity names it.
func TestComputeNoLedgerIsZeroWithReason(t *testing.T) {
	in := fixture(t)
	r := compute(t, in, "F-0001")
	if r.Counters != (Counters{}) || r.Reason != "no session bound" {
		t.Fatalf("report = %+v, want zero with reason", r)
	}
	bindAt(t, in.CacheDir, "s", "", "F-0002", at(1))
	if r := compute(t, in, "F-0009"); r.Counters != (Counters{}) || !strings.Contains(r.Reason, "F-0009") {
		t.Fatalf("report = %+v, want a reason naming F-0009", r)
	}
}

// TestTranscriptRootUnreadableIsError: a transcript root that exists but cannot
// be listed is an error, never read as "no transcript" (a denied read would
// otherwise look like pruning and print a false zero).
func TestTranscriptRootUnreadableIsError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 000 does not deny reads here")
	}
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "s", tmsg{ts: at(2), id: "a", req: "r", c: cc(1, 1, 1, 1)})
	bindAt(t, in.CacheDir, "s", "", "F-0001", at(1))
	projects := filepath.Join(in.Roots[0], "projects")
	if err := os.Chmod(projects, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(projects, 0o755) })

	if _, err := Compute(in, "F-0001"); !errors.Is(err, ErrTranscriptsUnreadable) || !strings.Contains(err.Error(), projects) {
		t.Fatalf("err = %v, want ErrTranscriptsUnreadable naming %s", err, projects)
	}
}
