package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

// newFeatureWithSpec births a feature via the one-shot path so the spec is
// real content and the doc gate is already open.
func newFeatureWithSpec(t *testing.T, projDir, title, specBody string) {
	t.Helper()
	specFile := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(specFile, []byte(specBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerne(t, "feature", "new", title, "--file", specFile, "--dir", projDir); err != nil {
		t.Fatalf("feature new --file: %v\noutput: %s", err, out)
	}
}

// TestApproveRefusesOpenClarifications pins F-0175 criterion 7: approve over a
// spec with open [ESCLARECER] markers refuses listing the questions; --force
// proceeds; a spec with only resolved markers passes untouched.
func TestApproveRefusesOpenClarifications(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	newFeatureWithSpec(t, projDir, "Com pergunta aberta",
		"# Com pergunta aberta\n\n## Problema\n\nContexto real.\n\n"+
			"[ESCLARECER: qual formato usar?]\n\n[ESCLARECER: onde persistir? | doc | sidecar]\n\n"+
			"## Verificação\n\nBarra: fixture de teste — referência nomeada\n")

	out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir)
	if err == nil {
		t.Fatalf("approve with open clarifications succeeded, want refusal:\n%s", out)
	}
	if !errors.Is(err, app.ErrOpenClarifications) {
		t.Fatalf("err = %v, want ErrOpenClarifications", err)
	}
	for _, q := range []string{"qual formato usar?", "onde persistir?"} {
		if !strings.Contains(err.Error(), q) {
			t.Errorf("error message missing question %q: %v", q, err)
		}
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateCandidate {
		t.Fatalf("state = %s, want candidate (refusal must not transition)", f.State)
	}

	// --force crosses the gate deliberately.
	out, err = execCerne(t, "feature", "approve", "F-0001", "--force", "--dir", projDir)
	if err != nil {
		t.Fatalf("approve --force: %v\noutput: %s", err, out)
	}
	f, err = store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateApproved {
		t.Fatalf("state = %s, want approved after --force", f.State)
	}
}

// TestApproveRefusesOpenClarificationsEnglish pins F-0182 criterion 7 — the
// end of the fail-open-by-language gate: an English spec with an open
// [CLARIFY] marker blocks approve exactly like a pt-BR one. Before the en
// vocabulary entered the catalog, this spec sailed through silently.
func TestApproveRefusesOpenClarificationsEnglish(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	newFeatureWithSpec(t, projDir, "English spec",
		"# English spec\n\n## Problema\n\nReal context in English.\n\n"+
			"[CLARIFY: which storage backend?]\n\n"+
			"## Verificação\n\nBarra: fixture de teste — referência nomeada\n")

	out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir)
	if err == nil {
		t.Fatalf("approve with open [CLARIFY] succeeded, want refusal:\n%s", out)
	}
	if !errors.Is(err, app.ErrOpenClarifications) {
		t.Fatalf("err = %v, want ErrOpenClarifications", err)
	}
	if !strings.Contains(err.Error(), "which storage backend?") {
		t.Errorf("error message missing the English question: %v", err)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateCandidate {
		t.Fatalf("state = %s, want candidate (refusal must not transition)", f.State)
	}
}

// TestApprovePassesResolvedClarifications pins criterion 5's data side: the
// resolved [ESCLARECIDO] trail does not block approve.
func TestApprovePassesResolvedClarifications(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	newFeatureWithSpec(t, projDir, "Só resolvidas",
		"# Só resolvidas\n\n## Problema\n\nContexto real.\n\n"+
			"[ESCLARECIDO: qual formato usar? → pipe inline]\n\n"+
			"## Verificação\n\nBarra: fixture de teste — referência nomeada\n")

	out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("approve with resolved markers: %v\noutput: %s", err, out)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.State != domain.StateApproved {
		t.Fatalf("state = %s, want approved", f.State)
	}
}
