package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTransitionApproveRefusesIncompleteSpec (F-0435 crit. 4): the UI approve
// goes through the same hard gate as the CLI.
func TestTransitionApproveRefusesIncompleteSpec(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	dir := filepath.Join(repoDir, "content", "features", "F-0001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := "---\ncerne_id: F-0001\n---\n\n# Título\n\n## Problema\n\nReal.\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\nPergunta ao juiz: qual é mais claro?\n"
	if err := os.WriteFile(filepath.Join(dir, "spec.md"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	commitBackend(t, repoDir)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/features/F-0001/transition", `{"action":"approve"}`, &got)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(got["error"], "missing template sections") {
		t.Fatalf("status = %d body %s, want 422 naming the missing sections", rec.Code, rec.Body.String())
	}
}
