package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/store"
)

// specBody is the revised spec used across the feature-spec tests.
const specBody = "---\ncerne_id: F-0001\n---\n\n## Problema\n\nVersão revisada pelo agente.\n\n## Verificação\n\nBarra: fixture de teste — referência nomeada\n"

// pipeStdinBody feeds body to readOptionalBody as if it had been piped in —
// the path the Backlog agent actually uses (it has no Write tool, so the spec
// arrives on stdin via heredoc, F-0265).
func pipeStdinBody(t *testing.T, body string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(body); err != nil {
		t.Fatal(err)
	}
	w.Close()
	t.Cleanup(func() { r.Close() })
	swapStdinBody(t, r)
}

// TestFeatureSpecFromStdin (F-0265 criterion 1): the verb rewrites a
// candidate's spec.md from a piped body, through the F-0256 core.
func TestFeatureSpecFromStdin(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}

	pipeStdinBody(t, specBody)
	out, err := execCerne(t, "feature", "spec", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature spec: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001 spec updated") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}
	got, err := os.ReadFile(app.FeatureDocPath(repoDir, "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if !strings.Contains(string(got), "Versão revisada pelo agente.") {
		t.Fatalf("spec not rewritten:\n%s", got)
	}
}

// TestFeatureSpecFromFile (criterion 1): --file is the human-side entry point.
func TestFeatureSpecFromFile(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	path := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(path, []byte(specBody), 0o644); err != nil {
		t.Fatal(err)
	}

	if out, err := execCerne(t, "feature", "spec", "F-0001", "--file", path, "--dir", projDir); err != nil {
		t.Fatalf("feature spec --file: %v\noutput: %s", err, out)
	}
	got, err := os.ReadFile(app.FeatureDocPath(repoDir, "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if !strings.Contains(string(got), "Versão revisada pelo agente.") {
		t.Fatalf("spec not rewritten:\n%s", got)
	}
}

// TestFeatureSpecRefusals pins the guards: past candidacy (criterion 2), empty
// body (criterion 3) and unknown id named in the error (criterion 11).
func TestFeatureSpecRefusals(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}

	t.Run("empty body", func(t *testing.T) {
		pipeStdinBody(t, "   \n")
		_, err := execCerne(t, "feature", "spec", "F-0001", "--dir", projDir)
		if err == nil || !strings.Contains(err.Error(), "empty spec body") {
			t.Fatalf("err = %v, want empty-body refusal", err)
		}
		got, _ := os.ReadFile(app.FeatureDocPath(repoDir, "F-0001", "spec.md"))
		if strings.Contains(string(got), "Versão revisada") {
			t.Fatal("empty body must not write")
		}
	})

	t.Run("unknown id", func(t *testing.T) {
		pipeStdinBody(t, specBody)
		_, err := execCerne(t, "feature", "spec", "F-9999", "--dir", projDir)
		if !errors.Is(err, store.ErrNotFound) || !strings.Contains(err.Error(), "F-9999") {
			t.Fatalf("err = %v, want ErrNotFound naming F-9999", err)
		}
	})

	t.Run("not candidate", func(t *testing.T) {
		// The creation-seeded spec has no Gauntlet bar; give it one so the
		// approve below exercises ErrNotCandidate, not the bar gate (F-0545).
		writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Spec aprovável."))
		commitBackend(t, repoDir)
		if out, err := execCerne(t, "feature", "approve", "F-0001", "--dir", projDir); err != nil {
			t.Fatalf("approve: %v\noutput: %s", err, out)
		}
		pipeStdinBody(t, specBody)
		_, err := execCerne(t, "feature", "spec", "F-0001", "--dir", projDir)
		if !errors.Is(err, app.ErrNotCandidate) {
			t.Fatalf("err = %v, want ErrNotCandidate", err)
		}
		got, _ := os.ReadFile(app.FeatureDocPath(repoDir, "F-0001", "spec.md"))
		if strings.Contains(string(got), "Versão revisada") {
			t.Fatal("approved spec must not be rewritten by the verb")
		}
	})
}
