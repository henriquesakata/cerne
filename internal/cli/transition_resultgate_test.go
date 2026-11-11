package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// startedFeature drives a fresh feature to in_development and returns it.
func startedFeature(t *testing.T) (projDir, repoDir, id string) {
	t.Helper()
	projDir, _, repoDir = setupProject(t)
	id = newFeature(t, projDir, "Feature A", 1)
	writeDoc(t, repoDir, id, "spec.md", realDoc(id, "Problema real."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "approve", id, "--dir", projDir); err != nil {
		t.Fatalf("approve: %v", err)
	}
	writeDoc(t, repoDir, id, "plan.md", realDoc(id, "Passo 1."))
	commitBackend(t, repoDir)
	if _, err := execCerne(t, "feature", "start", id, "--dir", projDir); err != nil {
		t.Fatalf("start: %v", err)
	}
	return projDir, repoDir, id
}

// TestCompleteRefusalMessageAndNoForce (F-0303 crit. 6): the refusal carries
// every pending item, the result path and the push instruction; --force does
// not cross it.
func TestCompleteRefusalMessageAndNoForce(t *testing.T) {
	projDir, repoDir, id := startedFeature(t)
	writeDoc(t, repoDir, id, "spec.md", "---\ncerne_id: "+id+"\n---\n\n# Título\n\n## Objetivo e critérios de aceitação\n\n1. um\n2. dois\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\n")
	writeDoc(t, repoDir, id, "result.md", "---\ncerne_id: "+id+"\n---\n\n# Título\n\nEntregue; critério 1 provado.\n")
	commitBackend(t, repoDir)

	_, err := execCerne(t, "feature", "complete", id, "--dir", projDir)
	if !errors.Is(err, app.ErrResultIncomplete) {
		t.Fatalf("err = %v, want ErrResultIncomplete", err)
	}
	t.Logf("refusal message:\n%s", err)
	for _, want := range []string{"2 pendência(s)", "critério(s) 2 da spec", "sem seção \"## Gauntlet\"", "content/features/" + id + "/result.md", "cerne push"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal %q lacks %q", err, want)
		}
	}
	if _, err := execCerne(t, "feature", "complete", id, "--force", "--dir", projDir); !errors.Is(err, app.ErrResultIncomplete) {
		t.Fatalf("--force crossed the result gate: %v", err)
	}
	if f, _ := store.LoadFeature(repoDir, id); f.State != domain.StateInDevelopment {
		t.Fatalf("state after refusals = %s, want in_development", f.State)
	}
}

// TestCompleteStubResultKeepsDocGate (F-0303 crit. 7): a stub result is still
// the document gate's refusal, not the new one.
func TestCompleteStubResultKeepsDocGate(t *testing.T) {
	projDir, _, id := startedFeature(t)
	_, err := execCerne(t, "feature", "complete", id, "--dir", projDir)
	if !errors.Is(err, domain.ErrGateResultRequired) {
		t.Fatalf("err = %v, want ErrGateResultRequired", err)
	}
	if errors.Is(err, app.ErrResultIncomplete) {
		t.Fatalf("stub result got the new refusal: %v", err)
	}
}
