package server

import (
	"net/http"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestTransitionApproveRequiresBar (F-0295 crit. 1): the UI approve path
// enforces the Gauntlet bar gate like the CLI — a real spec with no "Barra:"
// line is refused with the gate's teaching message and stays candidate.
func TestTransitionApproveRequiresBar(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	writeDoc(t, repoDir, "F-0001", "spec.md", "---\ncerne_id: F-0001\n---\n\n# Título\n\nProblema real descrito aqui.\n")
	commitBackend(t, repoDir)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition",
		`{"action":"approve"}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"barra do Gauntlet", "Barra: não aplicável", "Barra: dispensada por decisão humana"} {
		if !strings.Contains(got["error"], want) {
			t.Fatalf("error = %q, want it to contain %q", got["error"], want)
		}
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateCandidate {
		t.Fatalf("state after refused approve = %s, want candidate", f.State)
	}
}
