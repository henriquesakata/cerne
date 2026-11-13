package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

var otherLineRe = regexp.MustCompile(`cerne: (\d+) other findings outside your edits \(run cerne lint --staged --all for the list\)`)

// stagedDebtProject is a pulled project (F-0001, clean) plus old debt the user
// did not touch: F-0002 with a broken link, already in the backend.
func stagedDebtProject(t *testing.T) (projDir, specPath string) {
	t.Helper()
	projDir, _, repoDir, specPath := pulledProject(t)
	now := time.Now().UTC()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0002", Title: "Divida antiga", State: domain.StateCandidate, Facets: []string{"code"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	writeBackendDoc(t, repoDir, "content/features/F-0002/spec.md",
		"---\ncerne_id: F-0002\n---\n\n# Divida antiga\n\nVeja [x](./inexistente.md).\n")
	if _, err := gitback.CommitAndPush(repoDir, "seed old debt"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	return projDir, specPath
}

// debtSpecPath is the materialized spec of the old-debt feature F-0002.
func debtSpecPath(projDir string) string {
	return filepath.Join(projDir, ".docs", "current", "features", "F-0002-divida-antiga", "spec.md")
}

const otherHint = "run cerne lint --staged --all for the list"

// cleanSpec carries every template section and the bar, so it lints clean.
func cleanSpec(extra string) string {
	return "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\n## Problema\n\nDividir compras." + extra + "\n\n## Objetivo e critérios de aceitação\n\n1. Parcelar.\n\n## Decisões do design\n\nNenhuma.\n\n## Desenho\n\nSimples.\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\nMétodo: comparação cega\nSaída: vencer ou empatar\nPergunta ao juiz: qual é melhor?\n\n## Execução recomendada (agente)\n\nUm agente.\n\n## Fora de escopo\n\n- Nada.\n"
}

var brokenEdit = cleanSpec("\n\nver [detalhe](./sumiu.md)")

// F-0382 crit. 1+2: only the edit's findings are listed and fail; the rest is a count.
func TestLintStagedFailsOnlyOnEditedEntity(t *testing.T) {
	projDir, specPath := stagedDebtProject(t)
	mustWrite(t, specPath, brokenEdit)
	out, err := execCerne(t, "lint", "--staged", "--dir", projDir)
	if err == nil {
		t.Fatalf("want failure on the edit's broken link:\n%s", out)
	}
	if !strings.Contains(out, "sumiu.md") || strings.Contains(out, "inexistente.md") {
		t.Fatalf("listing must hold the edit's finding only:\n%s", out)
	}
	if m := otherLineRe.FindStringSubmatch(out); m == nil || m[1] == "0" {
		t.Fatalf("missing the outside-count line:\n%s", out)
	}
}

// F-0382 crit. 2: a clean edit passes even with old debt, and still counts it.
func TestLintStagedCleanEditPassesWithDebtCount(t *testing.T) {
	projDir, specPath := stagedDebtProject(t)
	mustWrite(t, specPath, cleanSpec(""))
	out, err := execCerne(t, "lint", "--staged", "--dir", projDir)
	if err != nil {
		t.Fatalf("old debt must not fail a clean edit: %v\n%s", err, out)
	}
	if !otherLineRe.MatchString(out) {
		t.Fatalf("missing the outside-count line:\n%s", out)
	}
}

// F-0382 crit. 3.
func TestLintStagedNoLocalEdits(t *testing.T) {
	projDir, _ := stagedDebtProject(t)
	out, err := execCerne(t, "lint", "--staged", "--dir", projDir)
	if err != nil || !strings.Contains(out, "cerne: no local edits to lint") {
		t.Fatalf("want exit 0 and the no-edits line, err=%v:\n%s", err, out)
	}
}

// F-0382 crit. 4: --all is today's behavior.
func TestLintStagedAllKeepsOldBehavior(t *testing.T) {
	projDir, specPath := stagedDebtProject(t)
	mustWrite(t, specPath, cleanSpec(""))
	out, err := execCerne(t, "lint", "--staged", "--all", "--dir", projDir)
	if err == nil || !strings.Contains(out, "inexistente.md") || otherLineRe.MatchString(out) {
		t.Fatalf("--all must list and fail on the old debt, err=%v:\n%s", err, out)
	}
}

// F-0382 crit. 6: --json carries both groups.
func TestLintStagedJSONObject(t *testing.T) {
	projDir, specPath := stagedDebtProject(t)
	mustWrite(t, specPath, brokenEdit)
	out, _ := execCerne(t, "lint", "--staged", "--json", "--dir", projDir)
	out, _, _ = strings.Cut(out, "\nError:")
	var got struct {
		Findings   []struct{ Rule, File string } `json:"findings"`
		OtherCount int                           `json:"other_count"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, out)
	}
	if len(got.Findings) != 1 || got.Findings[0].Rule != "broken-link" || got.OtherCount == 0 {
		t.Fatalf("got %+v", got)
	}
}

// F-0382 crit. 7: an explicit --feature scope wins over the edit set.
func TestLintStagedFeatureScopeWins(t *testing.T) {
	projDir, _ := stagedDebtProject(t)
	out, err := execCerne(t, "lint", "--staged", "--feature", "F-0002", "--dir", projDir)
	if err == nil || !strings.Contains(out, "inexistente.md") || otherLineRe.MatchString(out) || strings.Contains(out, "no local edits") {
		t.Fatalf("--feature must keep today's behavior, err=%v:\n%s", err, out)
	}
}

// Round 2 crit. 1: an old finding in an edited doc stays old debt (the line shifts, the key does not).
func TestLintStagedOldFindingInEditedDocStaysOther(t *testing.T) {
	projDir, _ := stagedDebtProject(t)
	p := debtSpecPath(projDir)
	b, _ := os.ReadFile(p)
	mustWrite(t, p, strings.Replace(string(b), "# Divida antiga\n", "# Divida antiga\n\nParagrafo novo.\n", 1))
	out, err := execCerne(t, "lint", "--staged", "--dir", projDir)
	if err != nil || strings.Contains(out, "inexistente.md") {
		t.Fatalf("old finding of an edited doc must not fail or be listed, err=%v:\n%s", err, out)
	}
	if m := otherLineRe.FindStringSubmatch(out); m == nil || !strings.Contains(out, otherHint) {
		t.Fatalf("want the count line with the --staged --all hint:\n%s", out)
	}
	if strings.Contains(out, "fixed by your edits") {
		t.Fatalf("nothing was fixed, no fixed line expected:\n%s", out)
	}
}

// Round 2 crit. 1: a detail change is a new finding (and the old one is fixed).
func TestLintStagedDetailChangeIsNew(t *testing.T) {
	projDir, _ := stagedDebtProject(t)
	p := debtSpecPath(projDir)
	b, _ := os.ReadFile(p)
	mustWrite(t, p, strings.Replace(string(b), "inexistente.md", "outro.md", 1))
	out, err := execCerne(t, "lint", "--staged", "--dir", projDir)
	if err == nil || !strings.Contains(out, "outro.md") || strings.Contains(out, "inexistente.md") {
		t.Fatalf("a changed detail must be listed as new, err=%v:\n%s", err, out)
	}
	if !strings.Contains(out, "cerne: 1 findings fixed by your edits") {
		t.Fatalf("the old detail is gone, want the fixed line:\n%s", out)
	}
}

// Round 2 crit. 1: an edit that clears an old finding says so.
func TestLintStagedFixedLine(t *testing.T) {
	projDir, _ := stagedDebtProject(t)
	p := debtSpecPath(projDir)
	b, _ := os.ReadFile(p)
	mustWrite(t, p, strings.Replace(string(b), "Veja [x](./inexistente.md).", "Sem link.", 1))
	out, err := execCerne(t, "lint", "--staged", "--dir", projDir)
	if err != nil || !strings.Contains(out, "cerne: 1 findings fixed by your edits") || strings.Contains(out, "inexistente.md") {
		t.Fatalf("want exit 0 and the fixed line, err=%v:\n%s", err, out)
	}
}

// Round 2: --json carries fixed_count.
func TestLintStagedJSONFixedCount(t *testing.T) {
	projDir, _ := stagedDebtProject(t)
	p := debtSpecPath(projDir)
	b, _ := os.ReadFile(p)
	mustWrite(t, p, strings.Replace(string(b), "Veja [x](./inexistente.md).", "Sem link.", 1))
	out, _ := execCerne(t, "lint", "--staged", "--json", "--dir", projDir)
	var got struct {
		FixedCount int `json:"fixed_count"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.FixedCount != 1 {
		t.Fatalf("want fixed_count 1, err=%v:\n%s", err, out)
	}
}

// Round 2, language bug: an English project's staged tree must print English section names.
func TestLintStagedFollowsDocsLanguage(t *testing.T) {
	projDir, _, repoDir, specPath := pulledProject(t)
	if err := store.SaveProject(repoDir, store.Project{Name: "demo", DocsLanguage: "en"}); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "docs language en"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, specPath, "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\n## Problem\n\nx\n")
	for _, args := range [][]string{{"lint", "--staged", "--all"}, {"lint", "--staged"}} {
		out, _ := execCerne(t, append(args, "--dir", projDir)...)
		if !strings.Contains(out, "Goal and acceptance criteria") || strings.Contains(out, "Objetivo e critérios") {
			t.Fatalf("%v: want English section names:\n%s", args, out)
		}
	}
}

func TestLintAllNeedsStaged(t *testing.T) {
	projDir, _ := stagedDebtProject(t)
	out, err := execCerne(t, "lint", "--all", "--dir", projDir)
	if err == nil || !strings.Contains(out, "--all needs --staged") {
		t.Fatalf("want the --all needs --staged error, err=%v:\n%s", err, out)
	}
}

// Round 2 crit. 1 and 5: the diff of findings. A finding absent from the base is the
// edit's wherever it lands (a plan-older-than-spec on the plan after a spec edit);
// the line is not part of the identity; a base finding that vanished is fixed.
func TestDiffFindings(t *testing.T) {
	old := lint.Finding{Rule: lint.RuleBrokenLink, Entity: "F-1", File: "content/features/F-1/spec.md", Detail: "broken link a", Line: 3}
	moved := old
	moved.Line = 9
	plan := lint.Finding{Rule: lint.RulePlanOlderThanSpec, Entity: "F-1", File: "content/features/F-1/plan.md", Detail: "plan older"}
	gone := lint.Finding{Rule: lint.RuleBrokenLink, Entity: "F-2", File: "content/features/F-2/spec.md", Detail: "broken link b"}
	added, kept, fixed := diffFindings([]lint.Finding{moved, plan}, []lint.Finding{old, gone})
	if len(added) != 1 || added[0].Rule != lint.RulePlanOlderThanSpec || len(kept) != 1 || fixed != 1 {
		t.Fatalf("added=%v kept=%v fixed=%d", added, kept, fixed)
	}
}

// noLintOrCopy makes the project lint and the tree copy fail the test if called.
func noLintOrCopy(t *testing.T) {
	t.Helper()
	oldLint, oldCopy := projectLint, copyTree
	projectLint = func(string, string, project.Config) ([]lint.Finding, error) {
		t.Fatalf("project lint ran with no local edits")
		return nil, nil
	}
	copyTree = func(string, string) error {
		t.Fatalf("clone copied with no local edits")
		return nil
	}
	t.Cleanup(func() { projectLint, copyTree = oldLint, oldCopy })
}

// F-0393 crit. 1: no edits answers at once, without the copy or the project lint.
func TestLintStagedNoEditsSkipsLintAndCopy(t *testing.T) {
	projDir, _ := stagedDebtProject(t)
	noLintOrCopy(t)
	out, err := execCerne(t, "lint", "--staged", "--dir", projDir)
	if err != nil || strings.TrimSpace(out) != "cerne: no local edits to lint" {
		t.Fatalf("want exit 0 and only the no-edits line, err=%v:\n%s", err, out)
	}
}

// F-0393 crit. 2: --json with no edits is the fixed payload.
func TestLintStagedNoEditsJSONSkipsLint(t *testing.T) {
	projDir, _ := stagedDebtProject(t)
	noLintOrCopy(t)
	out, err := execCerne(t, "lint", "--staged", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("no edits must pass: %v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) != 2 || got["edits"] != float64(0) {
		t.Fatalf("want {findings: [], edits: 0}, err=%v got=%v", err, got)
	}
	if f, ok := got["findings"].([]any); !ok || len(f) != 0 {
		t.Fatalf("findings must be an empty array: %s", out)
	}
}

// F-0393 crit. 4: an edit to a frozen document is refused as before, not short-circuited.
func TestLintStagedFrozenEditRefused(t *testing.T) {
	projDir, specPath := stagedDebtProject(t)
	ctx, err := resolvePaths(projDir)
	if err != nil {
		t.Fatalf("resolvePaths: %v", err)
	}
	repoDir := ctx.repoDir
	mustWrite(t, specPath, cleanSpec("\n\nmais"))
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	f.State = domain.StateImplemented
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	out, err := execCerne(t, "lint", "--staged", "--dir", projDir)
	if err == nil || strings.Contains(out, "no local edits") {
		t.Fatalf("want the frozen refusal, err=%v:\n%s", err, out)
	}
}

// F-0393 crit. 2: --json with edits carries the number of edited documents.
func TestLintStagedJSONEditsCount(t *testing.T) {
	projDir, specPath := stagedDebtProject(t)
	mustWrite(t, specPath, cleanSpec(""))
	out, _ := execCerne(t, "lint", "--staged", "--json", "--dir", projDir)
	var got struct {
		Edits *int `json:"edits"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Edits == nil || *got.Edits != 1 {
		t.Fatalf("want edits 1, err=%v:\n%s", err, out)
	}
}
