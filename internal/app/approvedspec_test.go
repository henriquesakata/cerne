package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/index"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/materialize"
	"gitlab.com/rockt/cerne/internal/store"
)

// approvedSpecA is the spec a test approves: trailing spaces and no final
// newline variation would be lost by any normalizing copy, so a byte-exact
// comparison pins the copy as a copy.
const approvedSpecA = "---\ncerne_id: F-0001\n---\n\n# Versão aprovada\n\nProblema real, com espaço no fim.  \n" + testBarTail

// approveFixture is a backend with F-0001 approved over approvedSpecA.
func approveFixture(t *testing.T) (repoDir, projDir, origin string) {
	t.Helper()
	repoDir, projDir, origin = newFeatureBackend(t)
	seedCandidateSpec(t, repoDir, "F-0001", approvedSpecA)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	return repoDir, projDir, origin
}

// TestApproveStoresApprovedSpec (F-0318 crit. 1): the approve keeps a
// byte-exact copy of the spec it approved, committed with the approval — so a
// fresh clone of the origin (another process, a pull, a restarted server)
// still reads the approved version after the live spec moved on.
func TestApproveStoresApprovedSpec(t *testing.T) {
	repoDir, projDir, origin := approveFixture(t)
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA+"\nLinha nova depois da aprovação.\n"); err != nil {
		t.Fatalf("SaveFeatureDoc: %v", err)
	}

	fresh := filepath.Join(t.TempDir(), "fresh")
	if err := gitback.Clone(origin, fresh); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	got, err := os.ReadFile(store.ApprovedSpecPath(fresh, "F-0001"))
	if err != nil {
		t.Fatalf("approved copy not in the origin: %v", err)
	}
	if string(got) != approvedSpecA {
		t.Fatalf("approved copy = %q, want the spec as approved %q", got, approvedSpecA)
	}
}

// TestApprovedCopyNotScanned (F-0318, plan task 1): the copy lives outside
// content/features/, so lint, the materializer and the index never read it — a
// broken link or a token that only the copy carries surfaces nowhere.
func TestApprovedCopyNotScanned(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	only := approvedSpecA + "\nzebraquartz [[F-9999]]\n"
	seedCandidateSpec(t, repoDir, "F-0001", only)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA); err != nil {
		t.Fatalf("SaveFeatureDoc: %v", err)
	}
	if got, err := os.ReadFile(store.ApprovedSpecPath(repoDir, "F-0001")); err != nil || !strings.Contains(string(got), "zebraquartz") {
		t.Fatalf("approved copy = %q, %v — the test needs the copy to carry the token", got, err)
	}

	findings, err := lint.Run(repoDir)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	for _, f := range findings {
		if strings.HasPrefix(f.File, "content/approved/") || strings.Contains(f.Detail, "F-9999") {
			t.Errorf("lint read the approved copy: %+v", f)
		}
	}

	docs := filepath.Join(t.TempDir(), ".docs")
	if _, err := materialize.Materialize(repoDir, docs, "", false); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	_ = filepath.WalkDir(docs, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if data, _ := os.ReadFile(p); strings.Contains(string(data), "zebraquartz") {
				t.Errorf("materialized %s carries the approved copy", p)
			}
		}
		return nil
	})

	db := filepath.Join(t.TempDir(), "index.db")
	if err := index.Rebuild(repoDir, db); err != nil {
		t.Fatalf("index: %v", err)
	}
	if hits, err := index.Search(db, "zebraquartz", 10); err != nil || len(hits) != 0 {
		t.Fatalf("index hits = %v, %v — the copy must not be indexed", hits, err)
	}
}

// approvedWithPlan is approveFixture with a real plan, so only the stale
// approval can stop a start.
func approvedWithPlan(t *testing.T) (repoDir, projDir string) {
	t.Helper()
	repoDir, projDir, _ = approveFixture(t)
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "plan", "# Plano\n\n## Tarefas\n\nPlano real, não um stub.\n"); err != nil {
		t.Fatalf("plan: %v", err)
	}
	return repoDir, projDir
}

// TestStartRefusesStaleApproval (F-0318 crit. 6, 3): a start over a spec that
// differs from the approved one is refused before anything is written — no
// state change, no work opened — naming the reason and the re-approve command.
// A spec edited back to the approved text (A→B→A) starts.
func TestStartRefusesStaleApproval(t *testing.T) {
	repoDir, projDir := approvedWithPlan(t)
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA+"\nCritério novo.\n"); err != nil {
		t.Fatalf("edit: %v", err)
	}

	_, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil)
	if !errors.Is(err, ErrApprovalStale) {
		t.Fatalf("start err = %v, want ErrApprovalStale", err)
	}
	for _, want := range []string{"spec changed since approval", "cerne feature approve F-0001"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err, want)
		}
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateApproved {
		t.Fatalf("state = %s after a refused start, want approved", f.State)
	}
	if works, _ := store.LoadAllWorks(repoDir); len(works) != 0 {
		t.Fatalf("a refused start opened works: %+v", works)
	}

	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA); err != nil {
		t.Fatalf("edit back: %v", err)
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil); err != nil {
		t.Fatalf("A→B→A start: %v", err)
	}
}

// TestStartPropagationSkipsStaleEpic (F-0318 crit. 6): a child's start pulls
// its ready epic along (propagateStartToAncestors) — never one whose approval
// went stale, or the propagation would be a way around the start gate.
func TestStartPropagationSkipsStaleEpic(t *testing.T) {
	repoDir, projDir := approvedWithPlan(t)
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", approvedSpecA+"\nCritério novo.\n"); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0002", Title: "Filha", State: domain.StateApproved, Parent: "F-0001"}); err != nil {
		t.Fatal(err)
	}
	writeFeatureDoc(t, repoDir, "F-0002", "plan.md", "# Plano\n\n## Tarefas\n\nPlano real da filha.\n")
	commitBackend(t, repoDir)

	out, err := TransitionFeature(repoDir, projDir, "F-0002", domain.ActionStart, "", "", nil)
	if err != nil {
		t.Fatalf("child start: %v", err)
	}
	if len(out.Propagated) != 0 {
		t.Fatalf("propagated = %v, want the stale epic left approved", out.Propagated)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateApproved {
		t.Fatalf("epic state = %s, want approved", f.State)
	}
}
