package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestBuildSpecDraftPrompt asserts the assembled prompt carries the feature's
// id, title and module so the model has the context it needs.
func TestBuildSpecDraftPrompt(t *testing.T) {
	f := store.Feature{ID: "F-0007", Title: "Conciliação bancária", Module: "finance", State: domain.StateCandidate}
	prompt := BuildSpecDraftPrompt(f, "# Conciliação bancária\n\n> Spec pendente")
	for _, want := range []string{"F-0007", "Conciliação bancária", "finance"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	// All canonical template sections must be requested, in spectmpl order.
	for _, want := range spectmpl.Sections {
		if !strings.Contains(prompt, "## "+want) {
			t.Fatalf("prompt missing required section %q:\n%s", want, prompt)
		}
	}
	// The prompt must teach the ambiguity marker and the EARS phrasing.
	for _, want := range []string{spectmpl.AmbiguityMarker, "O SISTEMA DEVE"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing authoring guidance %q:\n%s", want, prompt)
		}
	}
}

// TestSpecDraftByoKeyWritesAndCommits drives `cerne spec draft` in byo-key mode:
// the provider's body is written to the backend spec.md with cerne-stamped
// cerne_id frontmatter, origin advances and the lock snapshot is updated.
func TestSpecDraftByoKeyWritesAndCommits(t *testing.T) {
	projDir, origin, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPrompt = string(raw)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"## Problema\n\nFalta gerar boletos.\n\n## Objetivo e critérios de sucesso\n\nGerar boletos válidos."}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	lockBefore, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock before: %v", err)
	}
	headBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	out, err := execCerne(t, "spec", "draft", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("spec draft byo-key: %v\n%s", err, out)
	}

	// The provider received a prompt mentioning the feature.
	if !strings.Contains(gotPrompt, "Boletos") || !strings.Contains(gotPrompt, "F-0001") {
		t.Fatalf("provider prompt missing the feature id/title:\n%s", gotPrompt)
	}

	// The spec was written to the backend clone with the synthesized body...
	specPath := filepath.Join(repoDir, "content", "features", "F-0001", "spec.md")
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read drafted spec: %v", err)
	}
	if !strings.Contains(string(spec), "Falta gerar boletos.") {
		t.Fatalf("spec missing the synthesized body:\n%s", spec)
	}
	// ...and cerne-stamped cerne_id frontmatter (not trusted from the model).
	if !strings.HasPrefix(string(spec), "---\ncerne_id: F-0001\n---\n") {
		t.Fatalf("spec missing cerne_id frontmatter stamped by cerne:\n%s", spec)
	}

	// Origin advanced (push happened).
	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter == headBefore {
		t.Fatalf("origin HEAD did not advance after spec draft")
	}
	// Lock snapshot updated to the new origin HEAD.
	lockAfter, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock after: %v", err)
	}
	if lockAfter.Snapshot == lockBefore.Snapshot || lockAfter.Snapshot != headAfter {
		t.Fatalf("lock.Snapshot = %s, want new origin HEAD %s", lockAfter.Snapshot, headAfter)
	}

	// pt-BR confirmation.
	if !strings.Contains(out, "rascunhada e enviada") {
		t.Fatalf("spec draft output missing confirmation:\n%s", out)
	}
}

// TestSpecDraftUnknownFeature errors when the id does not exist in the backend.
func TestSpecDraftUnknownFeature(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, "http://127.0.0.1:0", "TEST_BYO_KEY")

	_, err := execCerne(t, "spec", "draft", "F-9999", "--dir", projDir)
	if err == nil {
		t.Fatalf("spec draft on unknown feature succeeded, want error")
	}
	if !strings.Contains(err.Error(), "F-9999") {
		t.Fatalf("error %q does not name the missing feature", err)
	}
}
