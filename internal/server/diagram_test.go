package server

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
)

// fakeDiagramProvider stands in for the LLM HTTP API: it records the prompt and
// replies with the given assistant text in the byo-key wire format.
func fakeDiagramProvider(t *testing.T, gotPrompt *string, replyText string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if gotPrompt != nil {
			*gotPrompt = string(raw)
		}
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"`+replyText+`"}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func configureByoKey(t *testing.T, projDir, baseURL string) {
	t.Helper()
	t.Setenv("TEST_DIAGRAM_KEY", "sk-x")
	if err := project.SaveConfig(projDir, project.Config{
		ProjectID: "proj-diagram",
		Backend:   "unused",
		LLM:       project.LLMConfig{Mode: "byo-key", BaseURL: baseURL, KeyEnv: "TEST_DIAGRAM_KEY"},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

// TestGenerateDiagramByoKey: with a provider configured, POST .../diagram
// synthesizes a Mermaid sequence diagram, saves it as sequence.mmd in the
// backend (advancing origin) and returns the generated text for the UI to
// convert and open.
func TestGenerateDiagramByoKey(t *testing.T) {
	repoDir, projDir, origin := newGitBackend(t) // seeds F-0001
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Fluxo de login."))

	var gotPrompt string
	srv := fakeDiagramProvider(t, &gotPrompt, `sequenceDiagram\n  A->>B: hi`)
	configureByoKey(t, projDir, srv.URL)

	head0, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	var got struct {
		Generated     bool   `json:"generated"`
		NeedsProvider bool   `json:"needs_provider"`
		Name          string `json:"name"`
		Mermaid       string `json:"mermaid"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-diagram"), http.MethodPost,
		"/api/features/F-0001/diagram", `{}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !got.Generated || got.NeedsProvider {
		t.Fatalf("generated=%v needs_provider=%v, want generated=true needs_provider=false", got.Generated, got.NeedsProvider)
	}
	if got.Name != "sequence.mmd" {
		t.Fatalf("name = %q, want sequence.mmd", got.Name)
	}
	if !strings.HasPrefix(got.Mermaid, "sequenceDiagram") {
		t.Fatalf("mermaid = %q, want a sequenceDiagram", got.Mermaid)
	}
	// The prompt carried the feature context (title and the current spec).
	if !strings.Contains(gotPrompt, "Feature A") {
		t.Fatalf("provider prompt missing the feature title:\n%s", gotPrompt)
	}
	// The .mmd artifact landed in the backend clone.
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "artifacts", "F-0001", "sequence.mmd"))
	if err != nil {
		t.Fatalf("sequence.mmd not written: %v", err)
	}
	if !strings.HasPrefix(string(data), "sequenceDiagram") {
		t.Fatalf("artifact body = %q, want the synthesized Mermaid", data)
	}
	// Origin advanced (commit + push happened).
	head1, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if head1 == head0 {
		t.Fatalf("origin HEAD did not advance after diagram generation")
	}
}

// TestGenerateDiagramDelegation: with no provider (delegation, the agent's
// mode), the endpoint can't generate for a human in the browser — it returns
// needs_provider and writes nothing.
func TestGenerateDiagramDelegation(t *testing.T) {
	repoDir, projDir := newBackend(t) // no .cernerc → LoadConfig fails → delegation
	seedFeature(t, repoDir, "F-0001", "Feature A", domain.StateCandidate)

	var got struct {
		Generated     bool   `json:"generated"`
		NeedsProvider bool   `json:"needs_provider"`
		Name          string `json:"name"`
		Mermaid       string `json:"mermaid"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-diagram-del"), http.MethodPost,
		"/api/features/F-0001/diagram", `{}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Generated || !got.NeedsProvider {
		t.Fatalf("generated=%v needs_provider=%v, want generated=false needs_provider=true", got.Generated, got.NeedsProvider)
	}
	if got.Mermaid != "" {
		t.Fatalf("mermaid = %q, want empty in delegation (no fabrication)", got.Mermaid)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "artifacts", "F-0001", "sequence.mmd")); err == nil {
		t.Fatalf("delegation mode must not write the artifact")
	}
}

// TestGenerateDiagramUnknownFeature: an id absent from the backend is a 404,
// regardless of provider mode (the feature is resolved first).
func TestGenerateDiagramUnknownFeature(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPost,
		"/api/features/F-9999/diagram", `{}`, &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] == "" {
		t.Fatalf("body = %v, want pt-BR error message", got)
	}
}

// TestGenerateDiagramNonSequence: when the model returns text that is not a
// Mermaid sequence diagram, the endpoint is a 422 and writes nothing.
func TestGenerateDiagramNonSequence(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t) // seeds F-0001
	srv := fakeDiagramProvider(t, nil, `flowchart TD\n  A-->B`)
	configureByoKey(t, projDir, srv.URL)

	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, "proj-diagram"), http.MethodPost,
		"/api/features/F-0001/diagram", `{}`, &got)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] == "" {
		t.Fatalf("body = %v, want pt-BR error message", got)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "artifacts", "F-0001", "sequence.mmd")); err == nil {
		t.Fatalf("non-sequence output must not write the artifact")
	}
}
