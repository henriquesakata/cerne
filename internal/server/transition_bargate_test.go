package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestTransitionApproveRequiresBar (F-0295 crit. 1): the UI approve path
// enforces the Gauntlet bar gate like the CLI — a real spec with no "Barra:"
// line is refused and stays candidate. The refusal is one line pointing at
// the check, which teaches the forms (F-0308 crit. 11); the page shows the
// readiness list before the click.
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
	for _, want := range []string{"Gauntlet bar", `no "Barra:" line`, "cerne feature approve F-0001 --check"} {
		if !strings.Contains(got["error"], want) {
			t.Fatalf("error = %q, want it to contain %q", got["error"], want)
		}
	}
	if strings.Contains(got["error"], "\n") {
		t.Fatalf("error spans several lines: %q", got["error"])
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateCandidate {
		t.Fatalf("state after refused approve = %s, want candidate", f.State)
	}
}
