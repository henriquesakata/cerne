package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/store"
)

var otherLineRe = regexp.MustCompile(`cerne: (\d+) other findings outside your edits \(run cerne lint for the list\)`)

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
	return projDir, specPath
}

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

// F-0382 crit. 5 and the entity-level fork: classification is by File, else by Entity.
func TestClassifyStagedFindings(t *testing.T) {
	edits := stagedEdits{
		files:    map[string]bool{"content/features/F-0001/spec.md": true},
		entities: map[string]bool{"F-0001": true},
	}
	in := []lint.Finding{
		{Rule: lint.RuleBrokenLink, Entity: "F-0001", File: "content/features/F-0001/spec.md"},
		{Rule: lint.RuleBrokenLink, Entity: "F-0009", File: "content/features/F-0001/spec.md"}, // edited doc, points elsewhere
		{Rule: lint.RuleMissingGateDoc, Entity: "F-0001"},                                      // entity-level
		{Rule: lint.RuleBrokenLink, Entity: "F-0002", File: "content/features/F-0002/spec.md"},
		{Rule: lint.RuleMissingGateDoc, Entity: "F-0002"},
	}
	mine, other := classifyStaged(in, edits)
	if len(mine) != 3 || len(other) != 2 {
		t.Fatalf("mine=%d other=%d, want 3 and 2", len(mine), len(other))
	}
}

func TestEditEntityFromBackendRel(t *testing.T) {
	for rel, want := range map[string]string{
		"content/features/F-0001/plan.md": "F-0001",
		"content/decisions/D-0003.md":     "D-0003",
		"content/notes/N-0007.md":         "N-0007",
	} {
		if got := editEntity(rel); got != want {
			t.Errorf("editEntity(%q) = %q, want %q", rel, got, want)
		}
	}
}
