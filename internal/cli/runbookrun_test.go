package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/index"
	"gitlab.com/rockt/cerne/internal/store"
)

// F-0873 fixture: imitates the RB-0070 Bloco 5a structure (S3 to S7 inside one
// block heading, S6.6 with no heading, S2/S9 in an unrelated title). The real
// RB-0070 is never read, written or run by any test.
const rbRunFixtureBody = "# Giro de credenciais\n\n## Releases entre o passo S2 e o S9\n\nTexto.\n\n" +
	"### Bloco 5a: giro (passos S3 a S7)\n\n- S3 confere\n- S4 gira\n- S5 sonda\n- S6 aplica\n- S6.6 parada\n- S7 fecha\n\n" +
	"```bash\n# S99 comentario\n```\n"

// runRunFixture creates RB-0001 from the fixture and opens a run on it.
func runRunFixture(t *testing.T, extra ...string) (projDir, repoDir, runID string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	bodyFile := filepath.Join(t.TempDir(), "rb.md")
	mustWrite(t, bodyFile, rbRunFixtureBody)
	rb(t, projDir, "runbook", "new", "Giro de credenciais", "--file", bodyFile)
	args := append([]string{"runbook", "run", "RB-0001", "--janela", "J3", "--passos", "S3,S4,S5,S6,S6.6,S7"}, extra...)
	out := rb(t, projDir, args...)
	if !strings.HasPrefix(out, "W-") {
		t.Fatalf("run must print the work id first:\n%s", out)
	}
	return projDir, repoDir, strings.Fields(out)[0]
}

// crit. 1: run opens a record with the steps of the runbook.
func TestRunbookRunOpensRecordWithSteps(t *testing.T) {
	projDir, _ := runbookFixture(t) // body with S6, S6.6, S6.7
	out := rb(t, projDir, "runbook", "run", "RB-0001", "--janela", "J1")
	id := strings.Fields(out)[0]
	for _, s := range []string{"S6", "S6.6", "S6.7"} {
		if !strings.Contains(out, s) {
			t.Fatalf("steps must come from the body, missing %s:\n%s", s, out)
		}
	}
	if strings.Contains(out, "S99") {
		t.Fatalf("no step from a code fence:\n%s", out)
	}
	if !strings.Contains(rb(t, projDir, "work", "list"), id) {
		t.Fatalf("the run must be an active work")
	}
	if _, err := execCerne(t, "runbook", "run", "RB-0001", "--janela", "J1", "--dir", projDir); err == nil {
		t.Fatal("a second active run of the same runbook window must be refused")
	}
	if _, err := execCerne(t, "runbook", "run", "RB-0001", "--dir", projDir); err == nil {
		t.Fatal("--janela is required")
	}
}

// The step list of the real shape: no heading per step, a range, noise in a title.
func TestRunbookRunStepsFromBlockHeadingBody(t *testing.T) {
	projDir, _, _ := setupProject(t)
	bodyFile := filepath.Join(t.TempDir(), "rb.md")
	mustWrite(t, bodyFile, rbRunFixtureBody)
	rb(t, projDir, "runbook", "new", "Giro de credenciais", "--file", bodyFile)
	out := rb(t, projDir, "runbook", "run", "RB-0001", "--janela", "J3")
	if !strings.Contains(out, "S2 S3 S4 S5 S6 S6.6 S7 S9") {
		t.Fatalf("range S3 a S7 and S6.6 must be steps:\n%s", out)
	}
}

// crit. 2: ok and pare, with UTC and BRT (-03:00), word quoted, nickname only.
func TestRunbookStepRecordsOkAndPareWithUTCAndBRT(t *testing.T) {
	projDir, repoDir, id := runRunFixture(t)
	rb(t, projDir, "runbook", "step", id, "S3", "--ok", "vai", "--por", "dono")
	rb(t, projDir, "runbook", "step", id, "S4", "--pare", "alerta firing: ClusterDown")
	run, err := store.LoadRun(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Entries) != 2 {
		t.Fatalf("entries = %d", len(run.Entries))
	}
	e := run.Entries[0]
	if e.State != "ok" || e.Word != "vai" || e.By != "dono" {
		t.Fatalf("ok entry = %+v", e)
	}
	if !strings.HasSuffix(e.UTC, "Z") || !strings.HasSuffix(e.BRT, "-03:00") {
		t.Fatalf("UTC/BRT = %q / %q", e.UTC, e.BRT)
	}
	if p := run.Entries[1]; p.State != "pare" || p.Output != "alerta firing: ClusterDown" {
		t.Fatalf("pare entry = %+v", p)
	}
	if _, err := execCerne(t, "runbook", "step", id, "S3", "--dir", projDir); err == nil {
		t.Fatal("neither --ok nor --pare must be refused")
	}
	if _, err := execCerne(t, "runbook", "step", id, "S3", "--ok", "a", "--pare", "b", "--dir", projDir); err == nil {
		t.Fatal("both --ok and --pare must be refused")
	}
	if _, err := execCerne(t, "runbook", "step", id, "S42", "--ok", "vai", "--dir", projDir); err == nil {
		t.Fatal("a step outside the run must be refused")
	}
	if _, err := execCerne(t, "runbook", "step", id, "S5", "--ok", "vai", "--por", "fulano@exemplo.com", "--dir", projDir); err == nil {
		t.Fatal("an e-mail as author must be refused")
	}
	if _, err := execCerne(t, "runbook", "step", id, "S5", "--ok", "vai, mande p/ fulano@exemplo.com", "--dir", projDir); err == nil {
		t.Fatal("an e-mail in the word must be refused")
	}
}

// crit. 3: only path and sha reach the backend; content never does, and
// content passed in place of a path is refused.
func TestRunbookStepEvidenceKeepsOnlyPathAndSha(t *testing.T) {
	projDir, repoDir, id := runRunFixture(t)
	const secret = "SENTINEL-PROD-LOG-DO-NOT-STORE-4711"
	file := filepath.Join(t.TempDir(), "evidence.log")
	mustWrite(t, file, secret+"\nlinha 2\n")
	sum := sha256.Sum256([]byte(secret + "\nlinha 2\n"))
	rb(t, projDir, "runbook", "step", id, "S3", "--ok", "vai", "--evidencia", file)
	run, _ := store.LoadRun(repoDir, id)
	ev := run.Entries[0].Evidence
	if ev == nil || ev.Path != file || ev.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("evidence = %+v", ev)
	}
	raw, err := os.ReadFile(filepath.Join(repoDir, "content", "runs", id+".json"))
	if err != nil || strings.Contains(string(raw), secret) {
		t.Fatalf("content must never reach the run record (err %v)", err)
	}
	if out, err := execCerne(t, "runbook", "step", id, "S4", "--ok", "vai", "--evidencia", secret+"\nlinha 2", "--dir", projDir); err == nil {
		t.Fatalf("content in place of a path must be refused:\n%s", out)
	}
	if _, err := execCerne(t, "runbook", "step", id, "S4", "--ok", "vai", "--evidencia", filepath.Join(t.TempDir(), "nao-existe.log"), "--dir", projDir); err == nil {
		t.Fatal("a missing file must be refused")
	}
	if _, err := execCerne(t, "runbook", "step", id, "S4", "--ok", "vai", "--evidencia", t.TempDir(), "--dir", projDir); err == nil {
		t.Fatal("a directory must be refused")
	}
}

// crit. 4: PARE, out of order and a different word become errata drafts with
// Altera:, once; a clean step is not reported.
func TestRunbookReportDraftsErrataForDeviations(t *testing.T) {
	projDir, repoDir, id := runRunFixture(t, "--palavra", "vai")
	rb(t, projDir, "runbook", "step", id, "S3", "--ok", "vai")
	rb(t, projDir, "runbook", "step", id, "S4", "--ok", "Vai")     // same word, other case: clean
	rb(t, projDir, "runbook", "step", id, "S6", "--ok", "pode ir") // S5 has no ok: out of order + other word
	rb(t, projDir, "runbook", "step", id, "S6.6", "--pare", "alerta firing")
	out := rb(t, projDir, "runbook", "report", id)
	if !strings.Contains(out, "2 deviation(s)") || !strings.Contains(out, "out of order") ||
		!strings.Contains(out, "word differs") || !strings.Contains(out, "PARE") {
		t.Fatalf("report:\n%s", out)
	}
	if strings.Contains(out, " S3 ") || strings.Contains(out, " S4 ") {
		t.Fatalf("clean steps must not be reported:\n%s", out)
	}
	out = rb(t, projDir, "runbook", "report", id, "--errata")
	if !strings.Contains(out, "errata item(s) drafted on RB-0001") {
		t.Fatalf("report --errata:\n%s", out)
	}
	doc, _ := store.LoadErrata(repoDir, "RB-0001")
	items := store.ParseErrataItems(doc)
	if len(items) != 2 { // one item per deviating entry: S6 (out of order, other word) and S6.6 (PARE)
		t.Fatalf("items = %d\n%s", len(items), doc)
	}
	targets := map[string]bool{}
	for _, it := range items {
		targets[it.Target] = true
		if it.Decided() {
			t.Fatalf("a draft is a proposal: %+v", it)
		}
	}
	if !targets["S6"] || !targets["S6.6"] || targets["S3"] || targets["S4"] {
		t.Fatalf("Altera: targets = %v", targets)
	}
	// Idempotent.
	rb(t, projDir, "runbook", "report", id, "--errata")
	doc2, _ := store.LoadErrata(repoDir, "RB-0001")
	if len(store.ParseErrataItems(doc2)) != len(items) {
		t.Fatalf("a second report must not duplicate:\n%s", doc2)
	}
	// The consolidated view (F-0872) carries the drafts. Placement is by heading
	// (F-0872 anchor), so in this block-heading shape they are listed apart.
	view := rb(t, projDir, "show", "RB-0001", "--consolidado")
	if !strings.Contains(view, "E1") || !strings.Contains(view, "E2") || !strings.Contains(view, "step S6.6") {
		t.Fatalf("consolidated view must carry the drafts:\n%s", view)
	}
}

// crit. 5: the record is visible to another agent through show and work list;
// a closed run takes no more steps.
func TestShowResolvesRunAndWorkListShowsIt(t *testing.T) {
	projDir, _, id := runRunFixture(t)
	rb(t, projDir, "runbook", "step", id, "S3", "--ok", "vai", "--por", "dono")
	out := rb(t, projDir, "show", id)
	for _, want := range []string{id, "work", "RB-0001", "window: J3", "S3  #1 ok", `"vai"`, "by dono", "S4  pending", "-03:00"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show %s must contain %q:\n%s", id, want, out)
		}
	}
	if !strings.Contains(rb(t, projDir, "show", id, "--json"), `"run"`) {
		t.Fatal("show --json must carry the run")
	}
	list := rb(t, projDir, "work", "list")
	if !strings.Contains(list, id) || !strings.Contains(list, "refs: RB-0001") {
		t.Fatalf("work list:\n%s", list)
	}
	rb(t, projDir, "work", "done", id)
	if _, err := execCerne(t, "runbook", "step", id, "S4", "--ok", "vai", "--dir", projDir); err == nil {
		t.Fatal("a done run must refuse steps")
	}
	if !strings.Contains(rb(t, projDir, "show", id), "S3  #1 ok") {
		t.Fatal("the record stays readable after done")
	}
}

// content/runs/ sits outside what lint, pull (materializer) and the index read:
// the lint output is the same with and without a run on the backend.
func TestRunsDirDoesNotBreakLintPullAndIndex(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	bodyFile := filepath.Join(t.TempDir(), "rb.md")
	mustWrite(t, bodyFile, rbRunFixtureBody)
	rb(t, projDir, "runbook", "new", "Giro de credenciais", "--file", bodyFile)
	before, _ := execCerne(t, "lint", "--dir", projDir) // the bare fixture has its own findings
	out := rb(t, projDir, "runbook", "run", "RB-0001", "--janela", "J3", "--passos", "S3,S4")
	id := strings.Fields(out)[0]
	file := filepath.Join(t.TempDir(), "e.log")
	mustWrite(t, file, "x")
	rb(t, projDir, "runbook", "step", id, "S3", "--ok", "vai", "--evidencia", file)
	rb(t, projDir, "runbook", "step", id, "S4", "--pare", "parou")
	if _, err := os.Stat(filepath.Join(repoDir, "content", "runs", id+".json")); err != nil {
		t.Fatal(err)
	}
	// An open run that stopped is flagged on purpose (run-stalled); closed, it is not.
	if lint, _ := execCerne(t, "lint", "--dir", projDir); !strings.Contains(lint, "RUN-STALLED") {
		t.Fatalf("an open stopped run must be flagged:\n%s", lint)
	}
	rb(t, projDir, "work", "done", id)
	after, _ := execCerne(t, "lint", "--dir", projDir)
	if before != after || strings.Contains(after, id) || strings.Contains(after, "content/runs") {
		t.Fatalf("a run must not change lint:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	if err := index.Rebuild(repoDir, filepath.Join(t.TempDir(), "idx.db")); err != nil {
		t.Fatalf("index rebuild: %v", err)
	}
}
