package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestTransitionCompleteRequiresResultRecord (F-0303 crit. 5): the UI complete
// path enforces the result record gate like the CLI — a real result without
// the Gauntlet section is refused with 422 and the feature stays in_development.
func TestTransitionCompleteRequiresResultRecord(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateInDevelopment)
	writeDoc(t, repoDir, "F-0001", "result.md", "---\ncerne_id: F-0001\n---\n\n# Título\n\nEntregue conforme o plano.\n")
	commitBackend(t, repoDir)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition",
		`{"action":"complete"}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"result incompleto", "## Gauntlet", "content/features/F-0001/result.md", "cerne push"} {
		if !strings.Contains(got["error"], want) {
			t.Fatalf("error = %q, want it to contain %q", got["error"], want)
		}
	}
	if f, _ := store.LoadFeature(repoDir, "F-0001"); f.State != domain.StateInDevelopment {
		t.Fatalf("state after refused complete = %s, want in_development", f.State)
	}
}
