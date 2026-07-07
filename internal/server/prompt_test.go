package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

func seedPromptTemplateNote(t *testing.T, repoDir string) {
	t.Helper()
	if err := store.SaveNote(repoDir, store.Note{
		ID: "N-0001", Title: "Investigação de erro", State: domain.NoteActive,
		Components: []string{"prompt-template"},
	}); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}
	p := filepath.Join(repoDir, "content", "notes", "N-0001.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("Use a skill observability.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed template"); err != nil {
		t.Fatalf("seed CommitAndPush: %v", err)
	}
}

func TestPromptTypesEndpoint(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	seedPromptTemplateNote(t, repoDir)

	rec := httptest.NewRecorder()
	New(repoDir, projDir, "demo").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/prompt/types", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("types status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "investigacao-de-erro") {
		t.Fatalf("types missing seeded template: %s", rec.Body.String())
	}
}

func TestPromptComposeEndpoint(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	seedPromptTemplateNote(t, repoDir)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, "demo"), http.MethodPost, "/api/prompt/compose",
		`{"type":"investigacao-de-erro","text":"Investigar lentidão."}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("compose status = %d: %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"## Intenção", "Investigar lentidão.", "Use a skill observability."} {
		if !strings.Contains(got["prompt"], want) {
			t.Fatalf("prompt missing %q:\n%s", want, got["prompt"])
		}
	}
	if got["work_id"] != "" {
		t.Fatalf("work_id = %q, want empty without register", got["work_id"])
	}
}

func TestPromptComposeEndpointUnknownType(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	seedPromptTemplateNote(t, repoDir)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, "demo"), http.MethodPost, "/api/prompt/compose",
		`{"type":"nao-existe","text":"x"}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	if !strings.Contains(got["error"], "investigacao-de-erro") {
		t.Fatalf("error must list available types: %s", got["error"])
	}
}

func TestPromptComposeEndpointRegister(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	seedPromptTemplateNote(t, repoDir)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, "demo"), http.MethodPost, "/api/prompt/compose",
		`{"type":"investigacao-de-erro","text":"Corrigir drift nos runbooks.","register":true}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("compose register status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(got["work_id"], "W-") {
		t.Fatalf("work_id = %q, want a W- id", got["work_id"])
	}
	if !strings.Contains(got["prompt"], "cerne work done "+got["work_id"]) {
		t.Fatalf("prompt missing closing instruction for %s", got["work_id"])
	}
	w, err := store.LoadWork(repoDir, got["work_id"])
	if err != nil {
		t.Fatalf("LoadWork: %v", err)
	}
	if w.State != domain.WorkActive {
		t.Fatalf("work state = %s, want active", w.State)
	}
}
