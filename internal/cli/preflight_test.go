package cli

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0874 fixture. Never the real RB-0070.
const rbPreBody = "# Troca de credencial\n\n## Passos\n\n" +
	"- S1 prepara\n  Pré-condição S1: registro F-0001\n" +
	"- S2 aplica\n  Pré-condição S2: result F-0002\n  - **Pré-condição** (S2): mr rockt/hub!2150 merged\n" +
	"- S3 fecha\n  Pré-condição S3: shell verde em producao\n\n" +
	"Pré-condição: registro D-9999\n\n" +
	"```bash\nPré-condição S1: registro F-7777\n```\n"

// preFixture seeds F-0001 (no result) and F-0002 (no result), creates RB-0001
// from rbPreBody, and fakes the forge: MR 2150 is opened.
func preFixture(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	now := time.Now().UTC()
	for _, id := range []string{"F-0001", "F-0002"} {
		if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Feat " + id, State: domain.StateInDevelopment, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed preflight"); err != nil {
		t.Fatal(err)
	}
	bodyFile := filepath.Join(t.TempDir(), "rb.md")
	mustWrite(t, bodyFile, rbPreBody)
	rb(t, projDir, "runbook", "new", "Troca de credencial", "--file", bodyFile)
	old := app.MRState
	app.MRState = func(project, iid string) (string, error) {
		if project == "rockt/hub" && iid == "2150" {
			return "opened", nil
		}
		return "", errors.New("unknown mr")
	}
	t.Cleanup(func() { app.MRState = old })
	return projDir, repoDir
}

// crit. 1: a step declares preconditions in the body; fences are skipped.
func TestParsePreconditionsDeclaredPerStep(t *testing.T) {
	got := app.ParsePreconditions(rbPreBody)
	if len(got) != 5 {
		t.Fatalf("want 5 declarations (the one in a fence is not one), got %d: %+v", len(got), got)
	}
	type want struct{ step, kind string }
	for i, w := range []want{{"S1", "registro"}, {"S2", "result"}, {"S2", "mr"}, {"S3", "manual"}, {"", "registro"}} {
		if got[i].Step != w.step || got[i].Kind != w.kind {
			t.Fatalf("#%d = %+v, want %+v", i, got[i], w)
		}
	}
	if mr := got[2]; mr.Args[0] != "rockt/hub!2150" || mr.Args[1] != "merged" {
		t.Fatalf("mr args = %v", mr.Args)
	}
}

func treeDigest(t *testing.T, root string) [32]byte {
	t.Helper()
	h := sha256.New()
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		b, _ := os.ReadFile(p)
		h.Write([]byte(p))
		h.Write(b)
		return nil
	})
	var s [32]byte
	copy(s[:], h.Sum(nil))
	return s
}

// crit. 2: preflight lists the unmet preconditions up to the step, changes nothing.
func TestRunbookPreflightListsUnmetUpToStepAndChangesNothing(t *testing.T) {
	projDir, repoDir := preFixture(t)
	before := treeDigest(t, repoDir)
	out := rb(t, projDir, "runbook", "preflight", "RB-0001", "--ate", "S2")
	if !strings.Contains(out, "1 met, 3 pending, 0 manual") {
		t.Fatalf("summary:\n%s", out)
	}
	for _, want := range []string{"S1", "[met]", "registro F-0001", "[pending]", "F-0002 has no result", "rockt/hub!2150 is opened, wanted merged", "D-9999 not found"} {
		if !strings.Contains(out, want) {
			t.Fatalf("preflight must show %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "shell verde") {
		t.Fatalf("S3 is after --ate S2:\n%s", out)
	}
	if after := treeDigest(t, repoDir); after != before {
		t.Fatal("preflight must change nothing in the backend")
	}
	// Now the result exists and the MR is merged: those two become met.
	_ = os.MkdirAll(filepath.Join(repoDir, "content", "features", "F-0002"), 0o755)
	mustWrite(t, filepath.Join(repoDir, "content", "features", "F-0002", "result.md"),
		"---\ncerne_id: F-0002\n---\n\n# Resultado\n\nEntregue e verificado com evidencia, sem pendencias.\n")
	app.MRState = func(string, string) (string, error) { return "merged", nil }
	out = rb(t, projDir, "runbook", "preflight", "RB-0001", "--ate", "S2")
	if !strings.Contains(out, "F-0002 has a result") || !strings.Contains(out, "rockt/hub!2150 is merged") {
		t.Fatalf("now met:\n%s", out)
	}
	if _, err := execCerne(t, "runbook", "preflight", "RB-0001", "--ate", "S42", "--dir", projDir); err == nil {
		t.Fatal("--ate a step the runbook does not name must be refused")
	}
}

// crit. 4: what the cerne cannot verify is manual, never met; so is an
// unreachable forge.
func TestPreflightUnknownKindIsManualNeverMet(t *testing.T) {
	projDir, _ := preFixture(t)
	out := rb(t, projDir, "runbook", "preflight", "RB-0001", "--ate", "S3")
	if !strings.Contains(out, "[manual]  shell verde em producao") {
		t.Fatalf("an unknown kind must be manual:\n%s", out)
	}
	app.MRState = func(string, string) (string, error) { return "", errors.New("no network") }
	out = rb(t, projDir, "runbook", "preflight", "RB-0001", "--ate", "S2")
	if !strings.Contains(out, "[manual]") || !strings.Contains(out, "forge not reachable") || strings.Contains(out, "!2150 is") {
		t.Fatalf("an unreachable forge must be manual:\n%s", out)
	}
	for _, r := range app.CheckPreconditions("/nonexistent", app.ParsePreconditions("Pré-condição S1: ninguem sabe\n")) {
		if r.Status == app.PreMet {
			t.Fatal("manual must never be met")
		}
	}
}

// crit. 3: `next` shows the same warning when a run is in progress and its next
// step has an open precondition; silent otherwise.
func TestNextWarnsOpenPreconditionOfRunInProgress(t *testing.T) {
	projDir, _ := preFixture(t)
	if out := rb(t, projDir, "next"); strings.Contains(out, "RUNBOOK PREFLIGHT") {
		t.Fatalf("no run in progress, no warning:\n%s", out)
	}
	out := rb(t, projDir, "runbook", "run", "RB-0001", "--janela", "J3")
	id := strings.Fields(out)[0]
	next := rb(t, projDir, "next")
	for _, want := range []string{"RUNBOOK PREFLIGHT (" + id + " RB-0001 window J3, next step S1", "D-9999 not found"} {
		if !strings.Contains(next, want) {
			t.Fatalf("next must contain %q:\n%s", want, next)
		}
	}
	if !strings.Contains(rb(t, projDir, "next", "--json"), `"runbook_preflight"`) {
		t.Fatal("next --json must carry runbook_preflight")
	}
	// The next step moves to S2 once S1 has its ok: S1's precondition leaves the warning.
	rb(t, projDir, "runbook", "step", id, "S1", "--ok", "vai")
	next = rb(t, projDir, "next")
	if !strings.Contains(next, "next step S2") || strings.Contains(next, "registro F-0001") {
		t.Fatalf("warning must follow the next step:\n%s", next)
	}
	// Done run: no warning.
	rb(t, projDir, "work", "done", id)
	if strings.Contains(rb(t, projDir, "next"), "RUNBOOK PREFLIGHT") {
		t.Fatal("a done run must not warn")
	}
}

// Precondition lines are plain body text: they change nothing for lint.
func TestPreconditionLinesDoNotBreakLint(t *testing.T) {
	projDir, _, _ := setupProject(t)
	plain := filepath.Join(t.TempDir(), "a.md")
	mustWrite(t, plain, "# Troca\n\n- S1 prepara\n")
	rb(t, projDir, "runbook", "new", "Troca", "--file", plain)
	before, _ := execCerne(t, "lint", "--dir", projDir)
	with := filepath.Join(t.TempDir(), "b.md")
	mustWrite(t, with, rbPreBody)
	rb(t, projDir, "runbook", "new", "Troca com precondicao", "--file", with)
	after, _ := execCerne(t, "lint", "--dir", projDir)
	if strings.Contains(after, "recondi") || strings.Contains(after, "preflight") {
		t.Fatalf("lint must not mention preconditions:\n%s", after)
	}
	// Same rules, one more runbook with the same findings as the plain one.
	if strings.Count(after, "RB-0002") != strings.Count(before, "RB-0001") {
		t.Fatalf("lint treats RB-0002 differently from the plain RB-0001:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
