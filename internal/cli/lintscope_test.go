package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// seedScopeFixture saves approved features without a spec — one
// missing-gate-doc finding each, entity-level — in a small hierarchy:
// F-0001, F-0002 standalone; epic F-0010 > F-0011 > F-0012; F-0013 outside.
// The two epics of each chain also raise epic-unplanned: 12 findings in all.
// F-0020 and F-0021 point at each other as parents (a cycle).
func seedScopeFixture(t *testing.T, repoDir string) {
	t.Helper()
	now := time.Now().UTC()
	parents := map[string]string{"F-0011": "F-0010", "F-0012": "F-0011", "F-0020": "F-0021", "F-0021": "F-0020"}
	for _, id := range []string{"F-0001", "F-0002", "F-0010", "F-0011", "F-0012", "F-0013", "F-0020", "F-0021"} {
		f := store.Feature{ID: id, Title: "Feature " + id, State: domain.StateApproved, Parent: parents[id], Facets: []string{"code"}, CreatedAt: now, UpdatedAt: now}
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", id, err)
		}
	}
	// F-0003: implemented with complete gate docs — no finding at all.
	clean := store.Feature{ID: "F-0003", Title: "Limpa", State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, clean); err != nil {
		t.Fatalf("SaveFeature F-0003: %v", err)
	}
	for _, name := range []string{"spec.md", "plan.md", "result.md"} {
		writeBackendDoc(t, repoDir, "content/features/F-0003/"+name, realDoc("F-0003", "Conteúdo real de "+name))
	}
	d := store.Decision{ID: "D-0001", Title: "Adotar gateway", State: domain.DecisionActive, Facets: []string{"infra"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveDecision(repoDir, d); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed scope fixtures"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

func TestLintFeatureScopeCleanExitsZero(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedScopeFixture(t, repoDir)

	out, err := execCerne(t, "lint", "--feature", "F-0003", "--dir", projDir)
	if err != nil {
		t.Fatalf("clean scope must exit 0: %v\n%s", err, out)
	}
	want := "No problems found in F-0003 (12 problem(s) outside the scope — run 'cerne lint' for the full list).\n"
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}
}

func TestLintFeatureScopeFindings(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedScopeFixture(t, repoDir)

	out, err := execCerne(t, "lint", "--feature", "F-0001", "--dir", projDir)
	if err == nil || err.Error() != "1 problem(s) found" {
		t.Fatalf("err = %v, want '1 problem(s) found'\n%s", err, out)
	}
	want := "MISSING-GATE-DOC (1)\n  F-0001 — approved feature without a real spec.md\n\n" +
		"Total: 1 problem(s) in F-0001 (11 outside the scope).\nError: 1 problem(s) found\n"
	if out != want {
		t.Fatalf("output:\n%s\nwant:\n%s", out, want)
	}
	if strings.Contains(out, "F-0002") {
		t.Fatalf("out-of-scope finding listed:\n%s", out)
	}
}

func TestLintParentScopeIsTransitive(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedScopeFixture(t, repoDir)

	out, err := execCerne(t, "lint", "--parent", "F-0010", "--dir", projDir)
	if err == nil || err.Error() != "5 problem(s) found" {
		t.Fatalf("err = %v, want '5 problem(s) found'\n%s", err, out)
	}
	for _, id := range []string{"F-0010", "F-0011", "F-0012"} {
		if !strings.Contains(out, "  "+id+" — ") {
			t.Fatalf("%s missing from scope:\n%s", id, out)
		}
	}
	if strings.Contains(out, "F-0013") || strings.Contains(out, "F-0001 ") {
		t.Fatalf("out-of-scope finding listed:\n%s", out)
	}
	if !strings.Contains(out, "Total: 5 problem(s) in F-0010 and 2 descendant(s) (7 outside the scope).\n") {
		t.Fatalf("total line wrong:\n%s", out)
	}
}

func TestLintParentScopeCycleTerminates(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedScopeFixture(t, repoDir)

	out, err := execCerne(t, "lint", "--parent", "F-0020", "--dir", projDir)
	if err == nil || err.Error() != "4 problem(s) found" {
		t.Fatalf("err = %v, want '4 problem(s) found'\n%s", err, out)
	}
	if !strings.Contains(out, "in F-0020 and 1 descendant(s)") {
		t.Fatalf("label wrong:\n%s", out)
	}
}

// A child whose stored parent differs in case (feature new on a
// case-insensitive filesystem) is still a descendant.
func TestLintParentScopeMatchesParentCaseInsensitively(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedScopeFixture(t, repoDir)
	now := time.Now().UTC()
	child := store.Feature{ID: "F-0030", Title: "Filha", State: domain.StateApproved, Parent: "f-0013", Facets: []string{"code"}, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, child); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "lowercase parent"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "lint", "--parent", "F-0013", "--dir", projDir)
	if err == nil || !strings.Contains(out, "  F-0030 — ") || !strings.Contains(out, "in F-0013 and 1 descendant(s)") {
		t.Fatalf("lowercase-parent child missed: %v\n%s", err, out)
	}
}

func TestLintScopeCountsIncompleteDoc(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedScopeFixture(t, repoDir)
	// A real but section-less spec: incomplete-doc carries File only.
	writeBackendDoc(t, repoDir, "content/features/F-0001/spec.md", realDoc("F-0001", "Conteúdo."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "lint", "--feature", "F-0001", "--dir", projDir)
	if err == nil {
		t.Fatalf("expected the incomplete-doc finding to fail the scope\n%s", out)
	}
	if !strings.Contains(out, "INCOMPLETE-DOC (1)\n  content/features/F-0001/spec.md — ") {
		t.Fatalf("incomplete-doc not attributed to F-0001:\n%s", out)
	}
}

func TestLintScopeUnknownID(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedScopeFixture(t, repoDir)

	blank := map[string]string{"--feature": "--feature needs a feature id", "--parent": "--parent needs a feature id"}
	for flag, want := range blank {
		out, err := execCerne(t, "lint", flag, "  ", "--dir", projDir)
		if err == nil || err.Error() != want {
			t.Fatalf("%s blank: err = %v, want %q\n%s", flag, err, want, out)
		}
	}
	cases := []struct{ name, flag, id, wantID string }{
		{"feature", "--feature", "F-9999", "F-9999"},
		{"parent", "--parent", "F-9999", "F-9999"},
		{"decision id is not a feature", "--feature", "D-0001", "D-0001"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := execCerne(t, "lint", tc.flag, tc.id, "--dir", projDir)
			if err == nil || !strings.Contains(err.Error(), "not found in backend") || !strings.Contains(err.Error(), tc.wantID) {
				t.Fatalf("err = %v, want not-found naming %q", err, tc.wantID)
			}
			if strings.Contains(out, "No problems found") {
				t.Fatalf("false green:\n%s", out)
			}
		})
	}
}

func TestLintScopeNormalizesID(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedScopeFixture(t, repoDir)

	out, err := execCerne(t, "lint", "--feature", " f-0001 ", "--dir", projDir)
	if err == nil || !strings.Contains(out, "Total: 1 problem(s) in F-0001 (11 outside the scope).") {
		t.Fatalf("lowercase/space id not normalized: %v\n%s", err, out)
	}
}

func TestLintScopeReportsOutsideCount(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedScopeFixture(t, repoDir)

	out, _ := execCerne(t, "lint", "--feature", "F-0002", "--dir", projDir)
	if !strings.Contains(out, "(11 outside the scope).") {
		t.Fatalf("dirty scope lacks outside count:\n%s", out)
	}
	// Without a scope flag the output never mentions a scope.
	whole, _ := execCerne(t, "lint", "--dir", projDir)
	if strings.Contains(whole, "outside the scope") {
		t.Fatalf("unscoped output mentions scope:\n%s", whole)
	}
	if !strings.Contains(whole, "Total: 12 problem(s).\n") {
		t.Fatalf("unscoped total changed:\n%s", whole)
	}
}

func TestLintScopeStagedAndFixTasks(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedScopeFixture(t, repoDir)
	writeBackendDoc(t, repoDir, "content/features/F-0001/spec.md", realDoc("F-0001", "Conteúdo."))
	if _, err := gitback.CommitAndPush(repoDir, "spec F-0001"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	dirs, _ := filepath.Glob(filepath.Join(projDir, ".docs", "current", "features", "F-0001-*"))
	if len(dirs) != 1 {
		t.Fatalf("materialized F-0001 dir: %v", dirs)
	}
	specPath := filepath.Join(dirs[0], "spec.md")
	// Local-only defect in F-0001's spec.
	mustWrite(t, specPath, "---\ncerne_id: F-0001\n---\n\n# Parcelamento\n\nver [detalhe](./inexistente.md)\n")

	out, err := execCerne(t, "lint", "--staged", "--feature", "F-0001", "--fix-tasks", "--dir", projDir)
	if err == nil {
		t.Fatalf("staged scope should fail on the local broken link\n%s", out)
	}
	if !strings.Contains(out, "BROKEN-LINK") || !strings.Contains(out, "outside the scope") {
		t.Fatalf("staged scope output wrong:\n%s", out)
	}
	idx := strings.Index(out, "## Maintenance tasks")
	if idx < 0 {
		t.Fatalf("fix-tasks section missing:\n%s", out)
	}
	tasks := out[idx:]
	if strings.Contains(tasks, "F-0002") || strings.Contains(tasks, "F-0010") {
		t.Fatalf("fix-tasks lists out-of-scope entities:\n%s", tasks)
	}
	if !strings.Contains(tasks, "- [ ] content/features/F-0001/") {
		t.Fatalf("fix-tasks missing the scope's task:\n%s", tasks)
	}

	// --parent works the same under --staged (F-0013 has no children).
	out, err = execCerne(t, "lint", "--staged", "--parent", "F-0013", "--dir", projDir)
	if err == nil || !strings.Contains(out, "Total: 1 problem(s) in F-0013 and 0 descendant(s)") {
		t.Fatalf("staged --parent: %v\n%s", err, out)
	}
}

func TestLintScopeFlagsExclusive(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedScopeFixture(t, repoDir)

	out, err := execCerne(t, "lint", "--feature", "F-0001", "--parent", "F-0010", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "none of the others can be") {
		t.Fatalf("err = %v, want mutual-exclusion refusal\n%s", err, out)
	}
}
