package cost

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
)

// subagent writes one subagent transcript: its first message, an optional last
// reply and one usage message of c at minute n.
func subagent(t *testing.T, in Inputs, sess, agent string, first, last string, n int, c Counters) {
	t.Helper()
	lines := []string{ask(sess, agent, at(n-1), first)}
	if last != "" {
		lines = append(lines, reply(sess, agent, at(n+1), last))
	}
	lines = append(lines, tmsg{ts: at(n), id: sess + agent, req: "r", agent: agent, c: c}.line(sess))
	writeLines(t, in.Roots[0], "-tmp-p/"+sess+"/subagents/agent-"+agent+".jsonl", lines...)
}

// anchored saves feature id in state with a result stamped with counters c
// (and by_role when roles is not nil).
func anchored(t *testing.T, in Inputs, id string, state domain.FeatureState, c Counters, roles map[string]Counters) {
	t.Helper()
	saveFeature(t, in.RepoDir, id, state)
	r := stampReport(id, c, 50)
	r.ByRole = roles
	writeResult(t, in.RepoDir, id, mustStamp(t, resultDoc(id), r))
}

func mainMsg(t *testing.T, in Inputs, sess string) {
	t.Helper()
	writeLines(t, in.Roots[0], "-tmp-p/"+sess+".jsonl", tmsg{ts: at(1), id: "main-" + sess, req: "r", c: cc(1, 0, 0, 1)}.line(sess))
}

// TestBackfillRolesReports (F-0396 crit. 1-3, 10): the report classifies the
// subagents of the ledger sessions by transcript, counts each on the entity of
// its parent session's binding, writes nothing, and names what it leaves out:
// no binding (unattributed), more than one entity (ambiguous), roles already
// in the anchor, no anchor, and critic+judge above the anchor's total.
func TestBackfillRolesReports(t *testing.T) {
	in := fixture(t)
	big := cc(1000, 1000, 1000, 1000)
	anchored(t, in, "F-0001", domain.StateImplemented, big, nil)
	anchored(t, in, "F-0002", domain.StateDone, big, nil)
	anchored(t, in, "F-0003", domain.StateImplemented, big, nil)
	anchored(t, in, "F-0004", domain.StateImplemented, big, map[string]Counters{"critic": cc(1, 0, 0, 1)})
	saveFeature(t, in.RepoDir, "F-0005", domain.StateImplemented)
	anchored(t, in, "F-0006", domain.StateImplemented, cc(1, 0, 0, 1), nil)
	judgeAsk, verdict := "Leia /tmp/a.md", "ok\nVEREDITO: A"

	mainMsg(t, in, "s1") // F-0001: critic by fence + judge by verdict; a plain agent counts nowhere
	bindAt(t, in.CacheDir, "s1", "", "F-0001", at(1))
	subagent(t, in, "s1", "c", "Ataque:\n"+diffFence, "", 3, cc(2, 0, 0, 2))
	subagent(t, in, "s1", "j", judgeAsk, verdict, 5, cc(4, 0, 0, 4))
	subagent(t, in, "s1", "p", "Faça X", "feito", 7, cc(9, 0, 0, 9))

	mainMsg(t, in, "s2") // F-0002 (done): one judge
	bindAt(t, in.CacheDir, "s2", "", "F-0002", at(1))
	subagent(t, in, "s2", "j", judgeAsk, verdict, 5, cc(8, 0, 0, 8))

	mainMsg(t, in, "s3") // bound only after the subagent ran: unattributed
	bindAt(t, in.CacheDir, "s3", "", "F-0003", at(50))
	subagent(t, in, "s3", "j", judgeAsk, verdict, 5, cc(16, 0, 0, 16))

	mainMsg(t, in, "s4") // the subagent rebinds to F-0003 while it runs: ambiguous
	bindAt(t, in.CacheDir, "s4", "", "F-0002", at(1))
	bindAt(t, in.CacheDir, "s4", "a", "F-0003", at(6))
	writeLines(t, in.Roots[0], "-tmp-p/s4/subagents/agent-a.jsonl", ask("s4", "a", at(2), "Ataque:\n"+diffFence),
		tmsg{ts: at(3), id: "a1", req: "r", agent: "a", c: cc(1, 0, 0, 1)}.line("s4"),
		tmsg{ts: at(7), id: "a2", req: "r", agent: "a", c: cc(1, 0, 0, 1)}.line("s4"))

	mainMsg(t, in, "s5") // F-0004 has roles, F-0005 no anchor, F-0006 too small
	bindAt(t, in.CacheDir, "s5", "", "F-0004", at(1))
	subagent(t, in, "s5", "j", judgeAsk, verdict, 5, cc(1, 0, 0, 1))
	mainMsg(t, in, "s6")
	bindAt(t, in.CacheDir, "s6", "", "F-0005", at(1))
	subagent(t, in, "s6", "j", judgeAsk, verdict, 5, cc(1, 0, 0, 1))
	mainMsg(t, in, "s7")
	bindAt(t, in.CacheDir, "s7", "", "F-0006", at(1))
	subagent(t, in, "s7", "j", judgeAsk, verdict, 5, cc(5, 0, 0, 5))

	// The real criterion-10 case: the parent's main bind moves F-0008 -> F-0009
	// while the subagent runs; its own binds never change.
	mainMsg(t, in, "s8")
	bindAt(t, in.CacheDir, "s8", "", "F-0008", at(1))
	bindAt(t, in.CacheDir, "s8", "", "F-0009", at(6))
	writeLines(t, in.Roots[0], "-tmp-p/s8/subagents/agent-a.jsonl", ask("s8", "a", at(2), "Leia /tmp/a.md"),
		tmsg{ts: at(3), id: "b1", req: "r", agent: "a", c: cc(1, 0, 0, 1)}.line("s8"),
		reply("s8", "a", at(8), verdict),
		tmsg{ts: at(9), id: "b2", req: "r", agent: "a", c: cc(1, 0, 0, 1)}.line("s8"))

	// A subagent on one feature and an epic, or an unresolved work: ambiguous too.
	mainMsg(t, in, "s9")
	bindAt(t, in.CacheDir, "s9", "", "F-0010", at(1))
	bindAt(t, in.CacheDir, "s9", "a", "E-0001", at(6))
	bindAt(t, in.CacheDir, "s9", "b", "F-0010", at(1))
	bindAt(t, in.CacheDir, "s9", "b", "W-0001", at(6))
	for _, ag := range []string{"a", "b"} {
		writeLines(t, in.Roots[0], "-tmp-p/s9/subagents/agent-"+ag+".jsonl", ask("s9", ag, at(2), "Ataque:\n"+diffFence),
			tmsg{ts: at(3), id: "c1" + ag, req: "r", agent: ag, c: cc(1, 0, 0, 1)}.line("s9"),
			tmsg{ts: at(7), id: "c2" + ag, req: "r", agent: ag, c: cc(1, 0, 0, 1)}.line("s9"))
	}

	// Roles already in the anchor and the judge already bound: nothing to do.
	anchored(t, in, "F-0007", domain.StateImplemented, big, map[string]Counters{"critic": cc(1, 0, 0, 1)})
	mainMsg(t, in, "s10")
	bindAt(t, in.CacheDir, "s10", "", "F-0007", at(1))
	subagent(t, in, "s10", "j", "Leia", "", 5, cc(1, 0, 0, 1))
	writeLines(t, in.Roots[0], "-tmp-p/s10/subagents/agent-j.jsonl", ask("s10", "j", at(4), judgeAsk), reply("s10", "j", at(6), verdict),
		tmsg{ts: at(5), id: "s10j", req: "r", agent: "j", c: cc(1, 0, 0, 1)}.line("s10"))
	if err := AppendBind(in.CacheDir, Bind{Session: "s10", Agent: "j", Entity: "F-0007", Harness: HarnessClaudeCode, Role: "judge", Retro: true, TS: at(20)}); err != nil {
		t.Fatal(err)
	}

	// Half of a subagent's messages are before any bind of its own: partial.
	anchored(t, in, "F-0011", domain.StateImplemented, big, nil)
	mainMsg(t, in, "s11")
	bindAt(t, in.CacheDir, "s11", "a", "F-0011", at(5))
	writeLines(t, in.Roots[0], "-tmp-p/s11/subagents/agent-a.jsonl", ask("s11", "a", at(2), "Ataque:\n"+diffFence),
		tmsg{ts: at(3), id: "d1", req: "r", agent: "a", c: cc(1, 0, 0, 1)}.line("s11"),
		tmsg{ts: at(7), id: "d2", req: "r", agent: "a", c: cc(10, 0, 0, 10)}.line("s11"))

	before := snapshotTree(t, in)
	r, err := BackfillRoles(in, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshotTree(t, in), before) {
		t.Fatal("the report wrote into the cache or the backend")
	}
	byID := map[string]FeatureRoles{}
	for _, f := range r.Features {
		byID[f.ID] = f
	}
	for _, want := range []FeatureRoles{
		{ID: "F-0001", State: domain.StateImplemented, NeedsAnchor: true, Critic: cc(2, 0, 0, 2), Judge: cc(4, 0, 0, 4), Agents: []AgentRole{
			{Session: "s1", Agent: "c", Role: "critic"}, {Session: "s1", Agent: "j", Role: "judge"}}},
		{ID: "F-0002", State: domain.StateDone, NeedsAnchor: true, Judge: cc(8, 0, 0, 8), Agents: []AgentRole{{Session: "s2", Agent: "j", Role: "judge"}}},
		// anchor already has roles, the judge is not bound yet: only the bind
		{ID: "F-0004", State: domain.StateImplemented, Judge: cc(1, 0, 0, 1), Agents: []AgentRole{{Session: "s5", Agent: "j", Role: "judge"}}},
		{ID: "F-0011", State: domain.StateImplemented, NeedsAnchor: true, Critic: cc(10, 0, 0, 10), Agents: []AgentRole{{Session: "s11", Agent: "a", Role: "critic"}}},
	} {
		if !reflect.DeepEqual(byID[want.ID], want) {
			t.Errorf("%s = %+v\nwant %+v", want.ID, byID[want.ID], want)
		}
	}
	if len(r.Features) != 4 {
		t.Errorf("Features = %d, want 4: %+v", len(r.Features), r.Features)
	}
	if want := []AgentRole{{Session: "s11", Agent: "a", Role: "critic"}}; !reflect.DeepEqual(r.Partial, want) {
		t.Errorf("Partial = %+v, want %+v", r.Partial, want)
	}
	if r.Unattributed != 1 {
		t.Errorf("Unattributed = %d, want 1", r.Unattributed)
	}
	if want := []Ambiguity{
		{Session: "s4", Agent: "a", Role: "critic", Entities: []string{"F-0002", "F-0003"}},
		{Session: "s8", Agent: "a", Role: "judge", Entities: []string{"F-0008", "F-0009"}},
		{Session: "s9", Agent: "a", Role: "critic", Entities: []string{"E-0001", "F-0010"}},
		{Session: "s9", Agent: "b", Role: "critic", Entities: []string{"F-0010", "W-0001"}},
	}; !reflect.DeepEqual(r.Ambiguous, want) {
		t.Errorf("Ambiguous = %+v, want %+v", r.Ambiguous, want)
	}
	if !reflect.DeepEqual(r.HasRoles, []string{"F-0007"}) || !reflect.DeepEqual(r.NoAnchor, []string{"F-0005"}) || !reflect.DeepEqual(r.Inconsistent, []string{"F-0006"}) {
		t.Errorf("HasRoles %v, NoAnchor %v, Inconsistent %v", r.HasRoles, r.NoAnchor, r.Inconsistent)
	}

	// --since drops the subagents whose messages start before it: F-0001 loses its critic.
	if r, err := BackfillRoles(in, at(4)); err != nil || len(r.Features) != 3 || r.Features[0].Critic != (Counters{}) || r.Features[0].Judge != cc(4, 0, 0, 4) {
		t.Errorf("--since at(4): %+v, %v", r.Features, err)
	}
}

// snapshotTree is every file under the cache and the backend with its bytes,
// to prove a call wrote nothing.
func snapshotTree(t *testing.T, in Inputs) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range []string{in.CacheDir, in.RepoDir} {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				raw, _ := os.ReadFile(p)
				out[p] = string(raw)
			}
			return nil
		})
	}
	return out
}

// TestRolesEdit (F-0396 crit. 4, 5): the edit keeps the anchor's counters and
// adds the roles — in the result's block for an implemented feature, as an
// erratum text with the same block for a done one; tokens an older anchor
// counted as "other" move to the new roles; roles above the total refuse.
func TestRolesEdit(t *testing.T) {
	in := fixture(t)
	repo := in.RepoDir
	total := cc(1000, 0, 0, 1000)
	anchored(t, in, "F-0001", domain.StateImplemented, total, nil)
	anchored(t, in, "F-0002", domain.StateDone, total, map[string]Counters{"main": cc(100, 0, 0, 100), "other": cc(60, 0, 0, 60)})
	critic, judge := cc(10, 0, 0, 10), cc(20, 0, 0, 20)

	text, asResult, err := RolesEdit(repo, "F-0001", false, critic, judge)
	if err != nil || !asResult {
		t.Fatalf("implemented: %v, asResult %v", err, asResult)
	}
	a, ok := anchor(text)
	if !ok || a.Counters != total || a.ByRole["critic"] != critic || a.ByRole["judge"] != judge || len(a.ByRole) != 2 {
		t.Errorf("anchor = %+v (ok %v), want total kept and critic+judge", a, ok)
	}
	if !strings.Contains(text, "Por papel: critic (input 10 · cache_creation 0 · cache_read 0 · output 10); judge (input 20 · cache_creation 0 · cache_read 0 · output 20).\n") ||
		!strings.Contains(text, "## Gauntlet") || strings.Count(text, "## Custo") != 1 {
		t.Errorf("result lost its shape:\n%s", text)
	}
	if again, _, err := RolesEdit(repo, "F-0001", false, critic, judge); err != nil || again != text {
		t.Errorf("same edit twice differs (err %v)", err)
	}

	text, asResult, err = RolesEdit(repo, "F-0002", true, critic, judge)
	if err != nil || asResult {
		t.Fatalf("done: %v, asResult %v", err, asResult)
	}
	a, ok = anchor(text)
	want := map[string]Counters{"main": cc(100, 0, 0, 100), "other": cc(30, 0, 0, 30), "critic": critic, "judge": judge}
	if !ok || a.Counters != total || !reflect.DeepEqual(a.ByRole, want) {
		t.Errorf("erratum anchor = %+v (ok %v), want other reduced by the moved tokens: %+v", a.ByRole, ok, want)
	}

	if _, _, err := RolesEdit(repo, "F-0001", false, cc(900, 0, 0, 0), cc(200, 0, 0, 0)); !errors.Is(err, ErrBackfillInconsistent) {
		t.Errorf("above the total: %v, want ErrBackfillInconsistent", err)
	}
	saveFeature(t, repo, "F-0009", domain.StateImplemented)
	if _, _, err := RolesEdit(repo, "F-0009", false, critic, judge); !errors.Is(err, ErrBackfillNoAnchor) {
		t.Errorf("no anchor: %v, want ErrBackfillNoAnchor", err)
	}
}

// TestAttributeOtherUndoesABackfilledRole (F-0396): a wrong retro role is
// undone by a later `cost attribute --role other`: the later bind wins in the
// cost, and the backfill leaves the subagent alone.
func TestAttributeOtherUndoesABackfilledRole(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0001", domain.StateInDevelopment)
	mainMsg(t, in, "s1")
	bindAt(t, in.CacheDir, "s1", "", "F-0001", at(1))
	subagent(t, in, "s1", "j", "Leia", "ok\nVEREDITO: A", 5, cc(4, 0, 0, 4))
	retro := func(role string, n int) {
		if err := AppendBind(in.CacheDir, Bind{Session: "s1", Agent: "j", Entity: "F-0001", Harness: HarnessClaudeCode, Role: role, Retro: true, TS: at(n)}); err != nil {
			t.Fatal(err)
		}
	}
	retro("judge", 20)
	if r := compute(t, in, "F-0001"); r.ByRole["judge"] != cc(4, 0, 0, 4) {
		t.Fatalf("ByRole = %+v, want the judge", r.ByRole)
	}
	retro("other", 21)
	r := compute(t, in, "F-0001")
	if r.ByRole["judge"] != (Counters{}) || r.ByRole["other"] != cc(4, 0, 0, 4) {
		t.Errorf("ByRole = %+v, want the later bind (other) to win", r.ByRole)
	}
	if b, err := BackfillRoles(in, time.Time{}); err != nil || len(b.Features)+len(b.NoAnchor) != 0 {
		t.Errorf("backfill after the undo: %+v, %v; want nothing", b, err)
	}
}
