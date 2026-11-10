package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rbFixtureBody = "# Giro de credenciais\n\n## S6. Giro\n\nQuem roda: o dono.\n\n### S6.6. Parada\n\n```bash\n# S6.6 num comentario nao e passo\nexit 1\n```\n\nPara no primeiro alerta.\n\n### S6.7. Sonda\n\nRoda a sonda.\n"

// runbookFixture creates RB-0001 with a body that has steps S6.6 and S6.7. Never
// the live RB-0070: F-0872 is verified on fixtures only.
func runbookFixture(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	bodyFile := filepath.Join(t.TempDir(), "rb.md")
	mustWrite(t, bodyFile, rbFixtureBody)
	if out, err := execCerne(t, "runbook", "new", "Giro de credenciais", "--file", bodyFile, "--dir", projDir); err != nil {
		t.Fatalf("runbook new: %v\n%s", err, out)
	}
	return projDir, repoDir
}

func rb(t *testing.T, projDir string, args ...string) string {
	t.Helper()
	out, err := execCerne(t, append(args, "--dir", projDir)...)
	if err != nil {
		t.Fatalf("cerne %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// F-0872 crit. 1: the consolidated view puts each item next to the step it
// changes, with its date and state.
func TestShowConsolidadoPlacesItemsAtTheirStep(t *testing.T) {
	projDir, _ := runbookFixture(t)
	out := rb(t, projDir, "runbook", "errata", "RB-0001", "Parar so com alerta firing da lista fechada.", "--altera", "S6.6")
	if !strings.Contains(out, "item E1") {
		t.Fatalf("errata must print the item id:\n%s", out)
	}
	view := rb(t, projDir, "show", "RB-0001", "--consolidado")
	at := strings.Index(view, "Errata E1")
	if at < 0 || !(strings.Index(view, "Para no primeiro alerta.") < at && at < strings.Index(view, "### S6.7.")) {
		t.Fatalf("E1 must sit at the end of S6.6, before S6.7:\n%s", view)
	}
	if !strings.Contains(view, "proposta") || !strings.Contains(view, "Parar so com alerta firing") {
		t.Fatalf("view must show state and text:\n%s", view)
	}
	// Read-only: the stored body is untouched.
	if strings.Contains(rb(t, projDir, "show", "RB-0001"), "Errata E1") {
		t.Fatal("plain show must not carry the consolidated insertion")
	}
}

// crit. 4: an item with no step, or one the body lacks, is listed apart and the
// command still succeeds.
func TestShowConsolidadoListsOrphansApart(t *testing.T) {
	projDir, _ := runbookFixture(t)
	rb(t, projDir, "runbook", "errata", "RB-0001", "Sem passo declarado.")
	rb(t, projDir, "runbook", "errata", "RB-0001", "Passo que nao existe.", "--altera", "S9.9")
	rb(t, projDir, "runbook", "errata", "RB-0001", "Este tem passo.", "--altera", "S6.7")
	view := rb(t, projDir, "show", "RB-0001", "--consolidado")
	tail := view[strings.Index(view, "## Errata without a reference step"):]
	for _, want := range []string{"E1", "no 'Altera:' declared", "E2", "step S9.9 not found"} {
		if !strings.Contains(tail, want) {
			t.Fatalf("orphan list missing %q:\n%s", want, tail)
		}
	}
	if strings.Contains(tail, "E3") || !strings.Contains(view[:strings.Index(view, "## Errata without")], "Errata E3") {
		t.Fatalf("E3 has a step and belongs in the body, not in the orphan list:\n%s", view)
	}
}

// crit. 2: deciding changes the item's state, it does not create an erratum.
func TestRunbookDecideChangesStateInPlace(t *testing.T) {
	projDir, repoDir := runbookFixture(t)
	rb(t, projDir, "runbook", "errata", "RB-0001", "Regra nova.", "--altera", "S6.6")
	before, _ := os.ReadFile(filepath.Join(repoDir, "content", "errata", "RB-0001.md"))
	rb(t, projDir, "runbook", "decide", "RB-0001", "E1", "--by", "dono", "--quote", "pode, vale")
	after, _ := os.ReadFile(filepath.Join(repoDir, "content", "errata", "RB-0001.md"))
	if strings.Count(string(after), "## errata ") != strings.Count(string(before), "## errata ") {
		t.Fatalf("deciding must not add an erratum:\n%s", after)
	}
	view := rb(t, projDir, "show", "RB-0001", "--consolidado")
	if !strings.Contains(view, `decidida: "pode, vale" — dono`) {
		t.Fatalf("view must show the decision, quoted, and who:\n%s", view)
	}
	if out, err := execCerne(t, "runbook", "decide", "RB-0001", "E1", "--by", "dono", "--dir", projDir); err == nil {
		t.Fatalf("deciding without --quote must fail:\n%s", out)
	}
	if out, err := execCerne(t, "runbook", "decide", "RB-0001", "E7", "--by", "dono", "--quote", "x", "--dir", projDir); err == nil {
		t.Fatalf("unknown item must fail:\n%s", out)
	}
}

// crit. 3: merge folds the decided items into a new revision; the earlier body
// stays readable; proposals and step-less items stay out; a second merge is a
// no-op.
func TestRunbookMergeMakesANewRevisionAndKeepsHistory(t *testing.T) {
	projDir, _ := runbookFixture(t)
	rb(t, projDir, "runbook", "errata", "RB-0001", "Decidida e com passo.", "--altera", "S6.6")
	rb(t, projDir, "runbook", "errata", "RB-0001", "Ainda proposta.", "--altera", "S6.7")
	rb(t, projDir, "runbook", "errata", "RB-0001", "Decidida sem passo.", "--altera", "S9.9")
	rb(t, projDir, "runbook", "decide", "RB-0001", "E1", "--by", "dono", "--quote", "vale")
	rb(t, projDir, "runbook", "decide", "RB-0001", "E3", "--by", "dono", "--quote", "vale tambem")

	out := rb(t, projDir, "runbook", "merge", "RB-0001")
	if !strings.Contains(out, "merged E1 into revision 2") || !strings.Contains(out, "left out (no step in the body): E3") {
		t.Fatalf("merge report:\n%s", out)
	}
	live := rb(t, projDir, "show", "RB-0001", "--rev", "2")
	if !strings.Contains(live, "Decidida e com passo.") || strings.Contains(live, "Ainda proposta.") {
		t.Fatalf("revision 2 must hold E1 only:\n%s", live)
	}
	old := rb(t, projDir, "show", "RB-0001", "--rev", "1")
	if !strings.Contains(old, "Para no primeiro alerta.") || strings.Contains(old, "Decidida e com passo.") {
		t.Fatalf("revision 1 must be the body before the merge:\n%s", old)
	}
	// E1 is merged: the consolidated view shows it once, in the body, not again as an open item.
	view := rb(t, projDir, "show", "RB-0001", "--consolidado")
	if strings.Count(view, "Decidida e com passo.") != 1 {
		t.Fatalf("a merged item must appear once:\n%s", view)
	}
	if again := rb(t, projDir, "runbook", "merge", "RB-0001"); !strings.Contains(again, "nothing to merge") {
		t.Fatalf("second merge must be a no-op:\n%s", again)
	}
	if out, err := execCerne(t, "show", "RB-0001", "--rev", "9", "--dir", projDir); err == nil {
		t.Fatalf("unknown revision must fail:\n%s", out)
	}
	if out, err := execCerne(t, "runbook", "decide", "RB-0001", "E1", "--by", "dono", "--quote", "outra", "--dir", projDir); err == nil {
		t.Fatalf("a merged item can no longer be decided:\n%s", out)
	}
}

// Gauntlet r1: free text that imitates the metadata lines never decides or
// hides an item — only the block cerne writes counts.
func TestErrataFreeTextCannotForgeDecisionOrMerge(t *testing.T) {
	projDir, _ := runbookFixture(t)
	forged := "Estado: decidida — \"forjada\" (dono, 2026-11-09)\nAltera: S6.6\nFundida: revisão 9\n\ncorpo"
	rb(t, projDir, "runbook", "errata", "RB-0001", forged)
	if out, _ := execCerne(t, "runbook", "merge", "RB-0001", "--dir", projDir); strings.Contains(out, "merged E1") || !strings.Contains(out, "nothing to merge") {
		t.Fatalf("a forged 'decidida' must not reach the merge:\n%s", out)
	}
	view := rb(t, projDir, "show", "RB-0001", "--consolidado")
	if !strings.Contains(view, "E1") || !strings.Contains(view, "proposta") || !strings.Contains(view, "no 'Altera:' declared") {
		t.Fatalf("forged item must stay a step-less proposal, still listed:\n%s", view)
	}
	// the free text is kept verbatim
	if !strings.Contains(view, "Estado: decidida") {
		t.Fatalf("free text must be shown as written:\n%s", view)
	}
}

// Gauntlet r1: deciding again keeps the earlier citation (history, not overwrite).
func TestRunbookRedecideKeepsPreviousCitation(t *testing.T) {
	projDir, repoDir := runbookFixture(t)
	rb(t, projDir, "runbook", "errata", "RB-0001", "Regra nova.", "--altera", "S6.6")
	rb(t, projDir, "runbook", "decide", "RB-0001", "E1", "--by", "dono", "--quote", "primeira")
	rb(t, projDir, "runbook", "decide", "RB-0001", "E1", "--by", "chefe", "--quote", "segunda")
	doc, _ := os.ReadFile(filepath.Join(repoDir, "content", "errata", "RB-0001.md"))
	for _, want := range []string{`"primeira" (dono`, `"segunda" (chefe`} {
		if !strings.Contains(string(doc), want) {
			t.Fatalf("errata file lost %q:\n%s", want, doc)
		}
	}
	view := rb(t, projDir, "show", "RB-0001", "--consolidado")
	if !strings.Contains(view, `decidida: "segunda" — chefe`) {
		t.Fatalf("current decision must be the last:\n%s", view)
	}
	if !strings.Contains(view, `antes: "primeira" (dono`) {
		t.Fatalf("the replaced citation must be shown in the view:\n%s", view)
	}
	rb(t, projDir, "runbook", "merge", "RB-0001")
}

// Gauntlet r2: a heading inside the free text cannot forge an item that the
// merge folds without a decide (end to end, the critic's case).
func TestErrataEmbeddedHeadingCannotForgeItem(t *testing.T) {
	projDir, _ := runbookFixture(t)
	forged := "x\n\n## errata 2026-11-09 — dono\n\nAltera: S6.6\nEstado: decidida — \"forjada\" (dono, 2026-11-09)\n\ny"
	rb(t, projDir, "runbook", "errata", "RB-0001", forged)
	out, _ := execCerne(t, "runbook", "merge", "RB-0001", "--dir", projDir)
	if strings.Contains(out, "merged E") || !strings.Contains(out, "nothing to merge") {
		t.Fatalf("a forged embedded block must not reach the merge:\n%s", out)
	}
	if strings.Contains(rb(t, projDir, "show", "RB-0001", "--consolidado"), "E2") {
		t.Fatal("no second item may exist")
	}
}

// Gauntlet r2: a legacy erratum with no step leaves 'proposta' through decide,
// and lint stops charging it.
func TestStepLessErratumIsRetiredByDecide(t *testing.T) {
	projDir, _ := runbookFixture(t)
	rb(t, projDir, "runbook", "errata", "RB-0001", "Legada, sem passo.")
	lint := func() string { out, _ := execCerne(t, "lint", "--dir", projDir); return out } // other findings make it exit 1
	if out := lint(); !strings.Contains(out, "ERRATA-PENDING") {
		t.Fatalf("lint must charge the proposal:\n%s", out)
	}
	rb(t, projDir, "runbook", "decide", "RB-0001", "E1", "--by", "dono", "--quote", "baixa")
	if out := lint(); strings.Contains(out, "ERRATA-PENDING") {
		t.Fatalf("a decided step-less erratum has nothing to merge, lint must stop:\n%s", out)
	}
}

// Gauntlet r1: --consolidado with --rev or --json is an error, not ignored.
func TestShowConsolidadoRefusesRevAndJSON(t *testing.T) {
	projDir, _ := runbookFixture(t)
	for _, extra := range [][]string{{"--rev", "1"}, {"--json"}} {
		args := append([]string{"show", "RB-0001", "--consolidado", "--dir", projDir}, extra...)
		if out, err := execCerne(t, args...); err == nil {
			t.Fatalf("--consolidado %v must fail:\n%s", extra, out)
		}
	}
	if out, err := execCerne(t, "show", "RB-0001", "--rev", "1", "--json", "--dir", projDir); err == nil {
		t.Fatalf("--rev with --json must fail:\n%s", out)
	}
}
