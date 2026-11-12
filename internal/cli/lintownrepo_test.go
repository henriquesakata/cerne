package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// F-0355 crit. 9, through the command: a live feature's spec citing the PR of
// the project's own repository (the [delivery] commit_url's) draws no
// unregistered-source finding; the same path on another host does.
func TestLintCommandOwnRepoURLNotUnregisteredSource(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	rc := filepath.Join(projDir, ".cernerc")
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, "\n[delivery]\ncommit_url = \"https://github.com/o/r/commit/{sha}\"\n"...)
	if err := os.WriteFile(rc, data, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Viva", State: domain.StateCandidate, Facets: []string{"code"}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	writeBackendDoc(t, repoDir, "content/features/F-0001/spec.md",
		"---\ncerne_id: F-0001\n---\n\n# Viva\n\nEntrega: [PR](https://github.com/o/r/pull/12) e [commit](https://github.com/o/r/commit/abc1234)\n"+
			"e de fora: [outro](https://github.example/o/r/pull/12)\n")
	commitBackend(t, repoDir)

	out, _ := execCerne(t, "lint", "--dir", projDir)
	if !strings.Contains(out, "https://github.example/o/r/pull/12") {
		t.Fatalf("the other host must be flagged:\n%s", out)
	}
	for _, own := range []string{"https://github.com/o/r/pull/12", "https://github.com/o/r/commit/abc1234"} {
		if strings.Contains(out, "external URL "+own) {
			t.Fatalf("own-repo URL %s flagged:\n%s", own, out)
		}
	}
}
