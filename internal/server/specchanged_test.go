package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/lint"
	"gitlab.com/rockt/cerne/internal/store"
)

// writeApprovedCopy writes the spec copy feature id's last approve kept (F-0318).
func writeApprovedCopy(t *testing.T, repoDir, id, content string) {
	t.Helper()
	path := store.ApprovedSpecPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFeatureDetailSpecChangedAfterApproval pins F-0262 as F-0318 redefined
// it: the feature page warns that the approval is stale when the live spec
// differs from the approved copy — same derivation the lint rule uses.
func TestFeatureDetailSpecChangedAfterApproval(t *testing.T) {
	repoDir, projDir := newBackend(t)
	seedFeature(t, repoDir, "F-0001", "Editada após aprovar", domain.StateApproved)
	seedFeature(t, repoDir, "F-0002", "Aprovada intacta", domain.StateApproved)
	writeApprovedCopy(t, repoDir, "F-0001", "A\n")
	writeDoc(t, repoDir, "F-0001", "spec.md", "A\nB\n")
	writeApprovedCopy(t, repoDir, "F-0002", "A\n")
	writeDoc(t, repoDir, "F-0002", "spec.md", "A\n")

	var got struct {
		SpecChanged bool `json:"spec_changed_after_approval"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got)
	if rec.Code != http.StatusOK || !got.SpecChanged {
		t.Fatalf("F-0001: status %d, spec_changed_after_approval = %v, want true", rec.Code, got.SpecChanged)
	}
	got.SpecChanged = false
	rec = doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0002", "", &got)
	if rec.Code != http.StatusOK || got.SpecChanged {
		t.Fatalf("F-0002: status %d, spec_changed_after_approval = %v, want false", rec.Code, got.SpecChanged)
	}
}

// TestStaleApprovalOneSourceOfTruth (F-0318 crit. 4, 3): lint, the feature
// payload and the start gate read one fact. Approved over A, edited to B, all
// three accuse it; edited back to A — with edit events after the approval in
// the log — none does.
func TestStaleApprovalOneSourceOfTruth(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	specA := realDoc("F-0001", "Problema real, versão aprovada.")
	writeDoc(t, repoDir, "F-0001", "spec.md", specA)
	commitBackend(t, repoDir)
	if _, err := app.TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := app.SaveFeatureDoc(repoDir, projDir, "F-0001", "plan", realDoc("F-0001", "Plano real.")); err != nil {
		t.Fatalf("plan: %v", err)
	}

	surfaces := func() (finding, payload bool) {
		t.Helper()
		findings, err := lint.Run(repoDir)
		if err != nil {
			t.Fatalf("lint: %v", err)
		}
		for _, f := range findings {
			finding = finding || (f.Rule == lint.RuleSpecChangedAfterApproval && f.Entity == "F-0001")
		}
		var got struct {
			SpecChanged bool `json:"spec_changed_after_approval"`
		}
		if rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/features/F-0001", "", &got); rec.Code != http.StatusOK {
			t.Fatalf("detail status %d", rec.Code)
		}
		return finding, got.SpecChanged
	}

	if err := app.SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", realDoc("F-0001", "Problema real, com um critério novo.")); err != nil {
		t.Fatalf("A→B: %v", err)
	}
	finding, payload := surfaces()
	_, startErr := app.TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil)
	if !finding || !payload || !errors.Is(startErr, app.ErrApprovalStale) {
		t.Fatalf("A→B: lint %v, payload %v, start %v — want all three to accuse it", finding, payload, startErr)
	}

	if err := app.SaveFeatureDoc(repoDir, projDir, "F-0001", "spec", specA); err != nil {
		t.Fatalf("A→B→A: %v", err)
	}
	finding, payload = surfaces()
	_, startErr = app.TransitionFeature(repoDir, projDir, "F-0001", domain.ActionStart, "", "", nil)
	if finding || payload || startErr != nil {
		t.Fatalf("A→B→A: lint %v, payload %v, start %v — want none to accuse it", finding, payload, startErr)
	}
}
