package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func writeSpecFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const noBarSpec = "# Título\n\n## Problema\n\nProblema real descrito aqui.\n"

// TestFeatureNewPrintsReadiness (F-0308 crit. 5): a spec written by
// `feature new --file` ends with the feature's approve readiness — what still
// stops the approve, or that nothing does.
func TestFeatureNewPrintsReadiness(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := execCerne(t, "feature", "new", "Spec sem barra", "--file", writeSpecFile(t, noBarSpec), "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	for _, want := range []string{"Approve readiness: not approvable", "bar-missing", "cerne feature approve F-0001 --check"} {
		if !strings.Contains(out, want) {
			t.Fatalf("feature new output missing %q:\n%s", want, out)
		}
	}

	out, err = execCerne(t, "feature", "new", "Spec com barra", "--file", writeSpecFile(t, realDoc("F-0002", "Problema real descrito aqui.")), "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Approve readiness: approvable") || !strings.Contains(out, "Barra: fixture de teste — referência nomeada") {
		t.Fatalf("feature new output lacks the approvable readiness with its bar:\n%s", out)
	}
}

// A context-only creation writes no spec body: there is no readiness to tell
// yet — the reminder sends the agent to write the spec first.
func TestFeatureNewWithContextPrintsNoReadiness(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := execCerne(t, "feature", "new", "Só contexto", "--context", "porque sim", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	if strings.Contains(out, "Approve readiness") {
		t.Fatalf("context-only creation printed a readiness line:\n%s", out)
	}
}

// TestFeatureSpecPrintsReadiness (F-0308 crit. 5): rewriting a candidate's spec
// ends with the readiness of the new text.
func TestFeatureSpecPrintsReadiness(t *testing.T) {
	projDir, _, _ := setupProject(t)
	id := newFeature(t, projDir, "Feature reescrita", 1)
	out, err := execCerne(t, "feature", "spec", id, "--file", writeSpecFile(t, noBarSpec), "--dir", projDir)
	if err != nil {
		t.Fatalf("feature spec: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Approve readiness: not approvable") || !strings.Contains(out, "bar-missing") {
		t.Fatalf("feature spec output lacks the readiness:\n%s", out)
	}
	out, err = execCerne(t, "feature", "spec", id, "--file", writeSpecFile(t, realDoc(id, "Problema real descrito aqui.")), "--dir", projDir)
	if err != nil {
		t.Fatalf("feature spec: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Approve readiness: approvable") {
		t.Fatalf("feature spec output lacks the approvable readiness:\n%s", out)
	}
}

// Readiness only speaks of a candidate: past candidacy (a task is born
// approved) there is nothing to approve, and the line is not printed at all.
func TestPrintApproveReadinessSilentPastCandidacy(t *testing.T) {
	repoDir := t.TempDir()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "T-0001", Title: "t", State: domain.StateApproved}); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	printApproveReadiness(&b, repoDir, "T-0001")
	if b.Len() != 0 {
		t.Fatalf("readiness printed past candidacy: %q", b.String())
	}
}
