package app

import (
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
