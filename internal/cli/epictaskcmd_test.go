package cli

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/store"
)

// seedApprovedState drives a feature to approved directly (the human gate is
// out of band here) and pushes, so the next CLI operation sees it.
func seedApprovedState(t *testing.T, repoDir string, f store.Feature) {
	t.Helper()
	f.State = domain.StateApproved
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed approved"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}
}

func TestEpicNew(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	out, err := execCerne(t, "epic", "new", "Meu épico", "--context", "por quê", "--dir", projDir)
	if err != nil {
		t.Fatalf("epic new: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "E-0001 created (candidate)") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}
	e, err := store.LoadFeature(repoDir, "E-0001")
	if err != nil || e.State != domain.StateCandidate {
		t.Fatalf("LoadFeature = %+v, %v; want E-0001 candidate", e, err)
	}
}

func TestEpicNewRequiresContext(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "epic", "new", "Sem contexto", "--dir", projDir); err == nil {
		t.Fatalf("want the F-0120 context gate for epics too")
	}
}

func TestTaskNew(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "PRD", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	// Drive the PRD past candidacy so the task's committed-parent gate opens.
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	seedApprovedState(t, repoDir, f)

	out, err := execCerne(t, "task", "new", "Primeiro passo", "--parent", "F-0001", "--context", "ctx", "--dir", projDir)
	if err != nil {
		t.Fatalf("task new: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "T-0001 created (approved)") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}
	tk, err := store.LoadFeature(repoDir, "T-0001")
	if err != nil || tk.State != domain.StateApproved || tk.Parent != "F-0001" {
		t.Fatalf("LoadFeature = %+v, %v; want T-0001 approved under F-0001", tk, err)
	}
}

func TestTaskNewRequiresParent(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := execCerne(t, "task", "new", "Solta", "--context", "ctx", "--dir", projDir)
	if err == nil {
		t.Fatalf("want refusal without --parent, output: %s", out)
	}
}
