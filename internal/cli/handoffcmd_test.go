package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func writeBrief(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "brief.md")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// handoffProject opens F-0001 and W-0001 on it, and records a handoff on W-0001.
func handoffProject(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	t.Setenv("USER", "u")
	t.Setenv("CERNE_SESSION", "")
	newFeature(t, projDir, "Feature A", 1)
	mustCerne(t, "work", "start", "Operação longa de migração", "--ref", "F-0001", "--dir", projDir)
	out := mustCerne(t, "work", "handoff", "W-0001", "--file", writeBrief(t, "Brief da sessão anterior."),
		"--next", "rodar a suíte", "--evidence", "docs/roteiro.md,F-0001", "--dir", projDir)
	if want := "cerne: handoff HO-0001 (W-0001) — pass the code; the receiver runs: cerne handoff take HO-0001\n"; !strings.HasPrefix(out, want) {
		t.Fatalf("handoff output:\n%s", out)
	}
	return projDir, repoDir
}

// F-0339 crit. 2 and 3 through the CLI: refused with the reason, English.
func TestWorkHandoffRefusals(t *testing.T) {
	projDir, _, _ := setupProject(t)
	mustCerne(t, "work", "start", "Operação longa", "--dir", projDir)
	brief := writeBrief(t, "brief")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no next", []string{"--file", brief}, "--next"},
		{"temp evidence", []string{"--file", brief, "--next", "n", "--evidence", "/tmp/roteiro.md"}, "/tmp/roteiro.md"},
	} {
		out, err := execCerne(t, append([]string{"work", "handoff", "W-0001", "--dir", projDir}, tc.args...)...)
		if err == nil || !strings.Contains(out+err.Error(), tc.want) {
			t.Fatalf("%s: err %v, out %q, want mention of %q", tc.name, err, out, tc.want)
		}
	}
}

// F-0339 crit. 6 and 7: the crossing ref prints the last entry plus the --from
// hint; the guard's refusal carries it too.
func TestWorkStartShowsHandoff(t *testing.T) {
	projDir, _ := handoffProject(t)
	out := mustCerne(t, "work", "start", "Assunto paralelo qualquer", "--ref", "F-0001", "--dir", projDir)
	for _, want := range []string{"last handoff of W-0001", "Brief da sessão anterior.", "Próximo passo: rodar a suíte",
		"take it over: cerne handoff take HO-0001"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if out := mustCerne(t, "work", "start", "Sem relação com nada", "--dir", projDir); strings.Contains(out, "handoff") {
		t.Fatalf("no crossing, yet handoff printed:\n%s", out)
	}
	gout, err := execCerne(t, "work", "start", "Operação longa de migração", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "Próximo passo: rodar a suíte") {
		t.Fatalf("guard refusal = %v / %s, want the handoff in it", err, gout)
	}
}

// F-0339 crit. 9: show prints the legacy entries after the work's fields;
// --json has them in `legacy_handoffs` (F-0368).
func TestShowWorkHandoffs(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	mustCerne(t, "work", "start", "Operação longa de migração", "--dir", projDir)
	if err := store.AppendHandoff(repoDir, "W-0001", "Brief da sessão anterior.\n\nPróximo passo: rodar a suíte", "u", time.Now()); err != nil {
		t.Fatal(err)
	}
	out := mustCerne(t, "show", "W-0001", "--dir", projDir)
	if !strings.Contains(out, "## legacy handoffs") || !strings.Contains(out, "Brief da sessão anterior.") {
		t.Fatalf("show W-0001:\n%s", out)
	}
	var j struct {
		Handoffs []struct{ Date, Author, Text string } `json:"legacy_handoffs"`
	}
	if err := json.Unmarshal([]byte(mustCerne(t, "show", "W-0001", "--json", "--dir", projDir)), &j); err != nil {
		t.Fatal(err)
	}
	if len(j.Handoffs) != 1 || !strings.Contains(j.Handoffs[0].Text, "Próximo passo: rodar a suíte") {
		t.Fatalf("json handoffs = %+v", j.Handoffs)
	}
}

// F-0339 crit. 11, end to end on a temporary backend: handoff, a second start
// that reads it, --from taking the work over, the panel dropping W-A.
func TestHandoffRealCase(t *testing.T) {
	projDir, repoDir := handoffProject(t)
	if ctx := pulledContext(t, projDir); !strings.Contains(ctx, "— handoff HO-0001 pending\n") || !strings.Contains(ctx, "## Pending handoffs\n\n- HO-0001 pending ") {
		t.Fatalf("CONTEXT.md must list W-0001 with its pending handoff (F-0368 crit. 10):\n%s", ctx)
	}
	out := mustCerne(t, "work", "start", "Segunda sessão chegando", "--ref", "F-0001", "--dir", projDir)
	if !strings.Contains(out, "Brief da sessão anterior.") {
		t.Fatalf("second start without the handoff:\n%s", out)
	}
	out = mustCerne(t, "work", "start", "Retomada da migração", "--from", "W-0001", "--dir", projDir)
	if !strings.Contains(out, "W-0003 created") || !strings.Contains(out, "Brief da sessão anterior.") || strings.Contains(out, "take it over") ||
		!strings.Contains(out, "took HO-0001") {
		t.Fatalf("--from output:\n%s", out)
	}
	wantWork(t, repoDir, "W-0001", domain.WorkDone, "handed off to W-0003 via HO-0001")
	if ctx := pulledContext(t, projDir); strings.Contains(ctx, "W-0001") || !strings.Contains(ctx, "W-0003") {
		t.Fatalf("panel after --from:\n%s", ctx)
	}
}

// F-0368 crit. 3, 4 and 8 through the CLI: take prints the new work, the
// closed origin, the brief and the next step; a second take refuses with the
// why; cancel needs --reason.
func TestHandoffCmdTakeAndCancel(t *testing.T) {
	projDir, repoDir := handoffProject(t)
	out := mustCerne(t, "handoff", "take", "HO-0001", "--note", "li tudo", "--dir", projDir)
	for _, want := range []string{
		"HO-0001 taken — W-0002 created (active) — Operação longa de migração",
		"closed W-0001 (handed off to W-0002 via HO-0001)",
		"Brief da sessão anterior.",
		"next step: rodar a suíte",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("take output missing %q:\n%s", want, out)
		}
	}
	wantWork(t, repoDir, "W-0001", domain.WorkDone, "handed off to W-0002 via HO-0001")
	if _, err := execCerne(t, "handoff", "take", "HO-0001", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "was taken by") {
		t.Fatalf("second take err = %v", err)
	}
	mustCerne(t, "work", "handoff", "W-0002", "--file", writeBrief(t, "b"), "--next", "n", "--dir", projDir)
	if _, err := execCerne(t, "handoff", "cancel", "HO-0002", "--dir", projDir); err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("cancel without reason err = %v", err)
	}
	if out := mustCerne(t, "handoff", "cancel", "HO-0002", "--reason", "plano mudou", "--dir", projDir); !strings.Contains(out, "HO-0002 cancelled: plano mudou") {
		t.Fatalf("cancel output:\n%s", out)
	}
}

// F-0368 crit. 5 through the CLI: an origin no longer active is said so.
func TestHandoffCmdTakeOriginDone(t *testing.T) {
	projDir, _ := handoffProject(t)
	mustCerne(t, "work", "done", "W-0001", "--dir", projDir)
	out := mustCerne(t, "handoff", "take", "HO-0001", "--title", "Retomada", "--dir", projDir)
	if !strings.Contains(out, "W-0001 was already done: left as it is") || !strings.Contains(out, "W-0002 created (active) — Retomada") {
		t.Fatalf("take output:\n%s", out)
	}
}

// seedHandoffs writes six handoffs straight into the clone, on clock now.
func seedHandoffs(t *testing.T, repoDir string, now time.Time) {
	t.Helper()
	ago := func(d time.Duration) *time.Time { at := now.Add(-d); return &at }
	if err := store.SaveWork(repoDir, store.Work{ID: "W-1234", Title: "Migração", State: domain.WorkActive, Actor: "a", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	for _, h := range []store.Handoff{
		{ID: "HO-0001", State: domain.HandoffPending, FromWork: "W-1234", Actor: "agent:a@b", CreatedAt: *ago(3 * time.Hour)},
		{ID: "HO-0002", State: domain.HandoffTaken, FromWork: "W-1230", TakenBy: "u@h", TakenWork: "W-1240", TakenSession: "s1", TakenAt: ago(26 * time.Hour), CreatedAt: *ago(30 * time.Hour)},
		{ID: "HO-0003", State: domain.HandoffTaken, FromWork: "W-1100", TakenBy: "u@h", TakenWork: "W-1101", TakenAt: ago(20 * 24 * time.Hour), CreatedAt: *ago(21 * 24 * time.Hour)},
		{ID: "HO-0004", State: domain.HandoffSuperseded, FromWork: "W-1234", SupersededBy: "HO-0006", CreatedAt: *ago(5 * time.Hour)},
		{ID: "HO-0005", State: domain.HandoffCancelled, FromWork: "W-1200", CancelReason: "plano mudou", CreatedAt: *ago(48 * time.Hour), UpdatedAt: *ago(47 * time.Hour)},
		{ID: "HO-0006", State: domain.HandoffPending, FromWork: "W-1235", Actor: "x@y", CreatedAt: *ago(5 * time.Hour)},
	} {
		if err := store.SaveHandoff(repoDir, h); err != nil {
			t.Fatal(err)
		}
	}
}

// F-0368 crit. 7: pending first, oldest to newest, then the taken of the last
// 14 days; --state filters, --json carries every field. Fixed clock.
func TestHandoffCmdList(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	handoffNow = func() time.Time { return now }
	t.Cleanup(func() { handoffNow = time.Now })
	seedHandoffs(t, repoDir, now)

	got := mustCerne(t, "handoff", "list", "--dir", projDir)
	want := "HO-0006 pending 5h — W-1235 — from x@y\n" +
		"HO-0001 pending 3h — W-1234 Migração — from agent:a@b\n" +
		"HO-0002 taken 1d ago by u@h (W-1240) — from W-1230\n"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("list =\n%s\nwant\n%s", got, want)
	}
	for state, want := range map[string][]string{
		"taken":      {"HO-0002 taken 1d ago", "HO-0003 taken 20d ago"},
		"superseded": {"HO-0004 superseded by HO-0006 — from W-1234"},
		"cancelled":  {"HO-0005 cancelled 1d ago — from W-1200: plano mudou"},
		"all":        {"HO-0001", "HO-0002", "HO-0003", "HO-0004", "HO-0005", "HO-0006"},
	} {
		out := mustCerne(t, "handoff", "list", "--state", state, "--dir", projDir)
		for _, w := range want {
			if !strings.Contains(out, w) {
				t.Fatalf("--state %s missing %q:\n%s", state, w, out)
			}
		}
		if state == "taken" && strings.Contains(out, "pending") {
			t.Fatalf("--state taken lists a pending one:\n%s", out)
		}
	}
	if _, err := execCerne(t, "handoff", "list", "--state", "bogus", "--dir", projDir); err == nil {
		t.Fatal("--state bogus accepted")
	}
	var j []map[string]any
	if err := json.Unmarshal([]byte(mustCerne(t, "handoff", "list", "--state", "all", "--json", "--dir", projDir)), &j); err != nil {
		t.Fatal(err)
	}
	if len(j) != 6 {
		t.Fatalf("json = %d entries, want 6", len(j))
	}
	for _, h := range j {
		if h["id"] == "HO-0002" && (h["taken_session"] != "s1" || h["taken_work"] != "W-1240" || h["state"] != "taken") {
			t.Fatalf("json HO-0002 = %v", h)
		}
	}
}

// F-0368 crit. 9: show HO- prints the record and the brief; show W- lists the
// handoffs from and into it with their state, and the F-0339 entries under
// `legacy handoffs`; --json has both.
func TestShowHandoff(t *testing.T) {
	projDir, repoDir := handoffProject(t)
	out := mustCerne(t, "show", "HO-0001", "--dir", projDir)
	for _, want := range []string{"HO-0001  handoff  [pending]  from W-0001", "next: rodar a suíte", "refs: F-0001", "Brief da sessão anterior."} {
		if !strings.Contains(out, want) {
			t.Fatalf("show HO-0001 missing %q:\n%s", want, out)
		}
	}
	mustCerne(t, "handoff", "take", "HO-0001", "--dir", projDir)
	if out := mustCerne(t, "show", "HO-0001", "--dir", projDir); !strings.Contains(out, "taken by: ") || !strings.Contains(out, "(W-0002)") {
		t.Fatalf("show taken HO-0001:\n%s", out)
	}
	// A legacy F-0339 entry, written after the take's commit (a mutation resets the clone).
	if err := store.AppendHandoff(repoDir, "W-0001", "entrada antiga", "u", time.Now()); err != nil {
		t.Fatal(err)
	}
	out = mustCerne(t, "show", "W-0001", "--dir", projDir)
	for _, want := range []string{"## handoffs\nHO-0001 taken ", "## legacy handoffs\n## handoff ", "entrada antiga"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show W-0001 missing %q:\n%s", want, out)
		}
	}
	if out := mustCerne(t, "show", "W-0002", "--dir", projDir); !strings.Contains(out, "HO-0001 taken ") {
		t.Fatalf("show W-0002 must list the handoff it came from:\n%s", out)
	}
	var j struct {
		Handoffs []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"handoffs"`
		Legacy []struct{ Text string } `json:"legacy_handoffs"`
	}
	if err := json.Unmarshal([]byte(mustCerne(t, "show", "W-0001", "--json", "--dir", projDir)), &j); err != nil {
		t.Fatal(err)
	}
	if len(j.Handoffs) != 1 || j.Handoffs[0].ID != "HO-0001" || j.Handoffs[0].State != "taken" || len(j.Legacy) != 1 {
		t.Fatalf("json = %+v", j)
	}
}
