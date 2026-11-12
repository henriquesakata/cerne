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
// --json has them in `handoffs`.
func TestShowWorkHandoffs(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	mustCerne(t, "work", "start", "Operação longa de migração", "--dir", projDir)
	if err := store.AppendHandoff(repoDir, "W-0001", "Brief da sessão anterior.\n\nPróximo passo: rodar a suíte", "u", time.Now()); err != nil {
		t.Fatal(err)
	}
	out := mustCerne(t, "show", "W-0001", "--dir", projDir)
	if !strings.Contains(out, "## handoff") || !strings.Contains(out, "Brief da sessão anterior.") {
		t.Fatalf("show W-0001:\n%s", out)
	}
	var j struct {
		Handoffs []struct{ Date, Author, Text string } `json:"handoffs"`
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
	if ctx := pulledContext(t, projDir); !strings.Contains(ctx, "W-0001") || !strings.Contains(ctx, ": cerne show W-0001") {
		t.Fatalf("CONTEXT.md must list W-0001 with the handoff suffix:\n%s", ctx)
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
