package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestApproveRefusesIncompleteSpec (F-0435 crit. 4): the impediment is hard —
// `cerne feature approve` and `--force` both refuse it, and --check lists it.
func TestApproveRefusesIncompleteSpec(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	id := newFeature(t, projDir, "Spec cortada", 1)
	writeDocRaw(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n# Título\n\n## Problema\n\nProblema real descrito aqui.\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\nPergunta ao juiz: qual é mais claro?\n")
	commitBackend(t, repoDir)

	out, err := execCerne(t, "feature", "approve", id, "--check", "--dir", projDir)
	if !strings.Contains(out, "incomplete-spec") || !strings.Contains(out, "Fora de escopo") {
		t.Fatalf("--check does not list incomplete-spec with the sections (err %v):\n%s", err, out)
	}
	for _, args := range [][]string{{"feature", "approve", id, "--dir", projDir}, {"feature", "approve", id, "--force", "--dir", projDir}} {
		out, err := execCerne(t, args...)
		if !errors.Is(err, app.ErrSpecIncomplete) {
			t.Fatalf("%v: err = %v (output %s), want ErrSpecIncomplete", args, err, out)
		}
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateCandidate {
		t.Fatalf("state = %s, want candidate", f.State)
	}
}
