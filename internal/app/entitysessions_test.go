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

// writeTranscript writes a main transcript of sess under root, with a title
// and a folder, and the subagent files with their agentType.
func writeTranscript(t *testing.T, root, sess, title, cwd string, agents map[string]string) {
	t.Helper()
	dir := filepath.Join(root, "projects", "-tmp-p")
	if err := os.MkdirAll(filepath.Join(dir, sess, "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"custom-title","customTitle":"` + title + `"}` + "\n" + `{"type":"user","cwd":"` + cwd + `","gitBranch":"b-` + sess + `"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, sess+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for id, kind := range agents {
		base := filepath.Join(dir, sess, "subagents", "agent-"+id)
		if err := os.WriteFile(base+".jsonl", []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(base+".meta.json", []byte(`{"agentType":"`+kind+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func ledgerBind(t *testing.T, cacheDir string, b cost.Bind) {
	t.Helper()
	if b.Harness == "" {
		b.Harness = cost.HarnessClaudeCode
	}
	if err := cost.AppendBind(cacheDir, b); err != nil {
		t.Fatal(err)
	}
}

// TestEntitySessions (F-0486 crit. 2, 3, 4, 6): the sessions that worked on a
// card, one row each in order of their first touch — who opened it, who wrote
// each document (spec, plan and result by different sessions), who ran
// feature start, the critic/judge a retro bind names, the works that ref it —
// a subagent folded into its main session with its type, and a session with
// no transcript here still listed.
func TestEntitySessions(t *testing.T) {
	cacheDir, repoDir, root := t.TempDir(), t.TempDir(), t.TempDir()
	at := func(m int) time.Time { return chanT0.Add(time.Duration(m) * time.Minute) }
	saveChanWork(t, repoDir, "W-0010", []string{"F-0001"}, nil)
	bindTestWork(t, repoDir, "W-0011", domain.WorkDone) // closed, no ref to F-0001
	if err := store.SaveWork(repoDir, store.Work{ID: "W-0012", Title: "revisão", State: domain.WorkDone, Actor: "agent", Refs: []string{"F-0001"}, CreatedAt: chanT0, UpdatedAt: chanT0}); err != nil {
		t.Fatal(err)
	}

	mark := func(sess, entity, kind string, ts time.Time) {
		t.Helper()
		if err := cost.AppendMark(cacheDir, cost.Bind{Session: sess, Entity: entity, Role: kind, Harness: cost.HarnessClaudeCode, TS: ts}); err != nil {
			t.Fatal(err)
		}
	}
	mark("s-tl", "F-0001", "spec", at(0)) // feature new --file: the spec lands just before the opening
	mark("s-tl", "F-0001", RoleOpened, at(0).Add(time.Second))
	mark("s-other", "F-0002", RoleOpened, at(0))
	mark("s-plan", "F-0001", "plan", at(3)) // another session planned it
	mark("s-plan", "F-0001", "plan", at(4)) // a second push: one role
	mark("s-tl", "F-0001", "result", at(10))
	ledgerBind(t, cacheDir, cost.Bind{Session: "s-tl", Agent: "a-dev", Entity: "F-0001", TS: at(5)})                       // dev subagent ran feature start
	ledgerBind(t, cacheDir, cost.Bind{Session: "s-tl", Agent: "a-crit", Entity: "F-0001", Role: "critic", Retro: true, TS: at(9)}) // cost attribute
	ledgerBind(t, cacheDir, cost.Bind{Session: "s-rev", Entity: "W-0012", TS: at(7)})
	ledgerBind(t, cacheDir, cost.Bind{Session: "s-codex", Entity: "W-0010", Harness: "other", TS: at(8)})
	ledgerBind(t, cacheDir, cost.Bind{Session: "s-x", Entity: "W-0011", TS: at(1)}) // not about F-0001
	ledgerBind(t, cacheDir, cost.Bind{Session: "s-tl", Agent: "a-dev", Entity: "F-0001", TS: at(6)}) // start re-run: one role

	writeTranscript(t, root, "s-tl", "TL do lote", "/w/tl", map[string]string{"a-dev": "dev", "a-crit": "code-reviewer"})
	writeTranscript(t, root, "s-rev", "Revisão F-0001", "/w/rev", nil)

	got, err := EntitySessions(cacheDir, repoDir, "F-0001", []string{root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d sessions, want s-tl, s-plan, s-rev, s-codex: %+v", len(got), got)
	}
	tl, plan, rev, codex := got[0], got[1], got[2], got[3]
	if tl.Session != "s-tl" || plan.Session != "s-plan" || rev.Session != "s-rev" || codex.Session != "s-codex" {
		t.Fatalf("order = %s, %s, %s, %s; want by first touch", tl.Session, plan.Session, rev.Session, codex.Session)
	}
	if r := plan.Roles; len(r) != 1 || r[0].Kind != "plan" || !r[0].At.Equal(at(3)) {
		t.Fatalf("s-plan roles = %+v, want plan at its first push", r)
	}
	if tl.Info.Title != "TL do lote" || tl.Info.Cwd != "/w/tl" || !tl.Info.Found {
		t.Fatalf("s-tl info = %+v", tl.Info)
	}
	wantTL := []EntityRole{
		{Kind: RoleOpened, At: at(0).Add(time.Second)},
		{Kind: "spec", At: at(0)},
		{Kind: RoleImplemented, Agent: "a-dev", AgentType: "dev", At: at(5)},
		{Kind: "critic", Agent: "a-crit", AgentType: "code-reviewer", At: at(9)},
		{Kind: "result", At: at(10)},
	}
	if len(tl.Roles) != len(wantTL) {
		t.Fatalf("s-tl roles = %+v, want %+v", tl.Roles, wantTL)
	}
	for i, r := range wantTL {
		if g := tl.Roles[i]; g.Kind != r.Kind || g.Agent != r.Agent || g.AgentType != r.AgentType || !g.At.Equal(r.At) {
			t.Fatalf("s-tl role %d = %+v, want %+v", i, g, r)
		}
	}
	if r := rev.Roles; len(r) != 1 || r[0].Kind != RoleWork || r[0].Work != "W-0012" || r[0].WorkTitle != "revisão" {
		t.Fatalf("s-rev roles = %+v, want the closed work W-0012 that refs the card", r)
	}
	if codex.Harness != "other" || codex.Info.Found || codex.Roles[0].Work != "W-0010" {
		t.Fatalf("s-codex = %+v, want harness other, no transcript, work W-0010", codex)
	}

	none, err := EntitySessions(cacheDir, repoDir, "F-0099", []string{root}, nil)
	if err != nil || len(none) != 0 {
		t.Fatalf("F-0099 = %+v (%v), want no session", none, err)
	}
}
