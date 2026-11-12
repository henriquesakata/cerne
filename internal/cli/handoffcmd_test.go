package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
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
	if !strings.Contains(out, "W-0001: handoff recorded") {
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
		`cerne work start "<description>" --from W-0001`} {
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

// F-0339 crit. 9: show prints the entries after the work's fields; --json has
// them in `handoffs`.
func TestShowWorkHandoffs(t *testing.T) {
	projDir, _ := handoffProject(t)
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
	if !strings.Contains(out, "W-0003 created") || !strings.Contains(out, "Brief da sessão anterior.") || strings.Contains(out, "--from W-0001") {
		t.Fatalf("--from output:\n%s", out)
	}
	wantWork(t, repoDir, "W-0001", domain.WorkDone, "handed off to W-0003")
	if ctx := pulledContext(t, projDir); strings.Contains(ctx, "W-0001") || !strings.Contains(ctx, "W-0003") {
		t.Fatalf("panel after --from:\n%s", ctx)
	}
}
