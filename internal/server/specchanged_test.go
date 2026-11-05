package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
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
