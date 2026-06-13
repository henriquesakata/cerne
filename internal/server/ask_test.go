package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
)

// askSourceJSON mirrors the snake_case source projection in the /api/ask body.
type askSourceJSON struct {
	Source string `json:"source"`
	Kind   string `json:"kind"`
	State  string `json:"state"`
}

// TestAskEndpointByoKeyAnswers: with a provider configured in .cernerc, the
// endpoint calls it and returns the synthesized answer plus the retrieved
// sources — the human in the browser gets a real answer, not a prompt.
func TestAskEndpointByoKeyAnswers(t *testing.T) {
	repoDir, projDir := newBackend(t)
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	seedFeature(t, repoDir, "F-0001", "Boletos recorrentes", domain.StateImplemented)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Pagamento de boletos mensais no satélite."))

	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPrompt = string(raw)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"Boletos geram cobranças bancárias."}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_ASK_KEY", "sk-x")
	if err := project.SaveConfig(projDir, project.Config{
		ProjectID: "proj-ask",
		Backend:   "unused",
		LLM:       project.LLMConfig{Mode: "byo-key", BaseURL: srv.URL, KeyEnv: "TEST_ASK_KEY"},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	var got struct {
		Answered      bool            `json:"answered"`
		NeedsProvider bool            `json:"needs_provider"`
		Answer        string          `json:"answer"`
		Sources       []askSourceJSON `json:"sources"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-ask"), http.MethodPost, "/api/ask",
		`{"query":"o que é boletos?","history":false}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !got.Answered || got.NeedsProvider {
		t.Fatalf("answered=%v needs_provider=%v, want answered=true needs_provider=false", got.Answered, got.NeedsProvider)
	}
	if !strings.Contains(got.Answer, "cobranças bancárias") {
		t.Fatalf("answer = %q, want the provider text", got.Answer)
	}
	if !strings.Contains(gotPrompt, "boletos") {
		t.Fatalf("provider prompt missing the question:\n%s", gotPrompt)
	}
	if len(got.Sources) == 0 {
		t.Fatalf("sources empty, want the F-0001 hit")
	}
}

// TestAskEndpointDelegationReturnsSources: with no provider (delegation, the
// agent's mode), the server can't answer for a human — it degrades to the
// retrieved sources plus needs_provider, and never fabricates an answer.
func TestAskEndpointDelegationReturnsSources(t *testing.T) {
	repoDir, projDir := newBackend(t) // no .cernerc → LoadConfig fails → delegation
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	seedFeature(t, repoDir, "F-0001", "Boletos recorrentes", domain.StateImplemented)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Pagamento de boletos mensais no satélite."))

	var got struct {
		Answered      bool            `json:"answered"`
		NeedsProvider bool            `json:"needs_provider"`
		Answer        string          `json:"answer"`
		Sources       []askSourceJSON `json:"sources"`
	}
	rec := doJSON(t, New(repoDir, projDir, "proj-ask-del"), http.MethodPost, "/api/ask",
		`{"query":"boletos"}`, &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Answered || !got.NeedsProvider {
		t.Fatalf("answered=%v needs_provider=%v, want answered=false needs_provider=true", got.Answered, got.NeedsProvider)
	}
	if got.Answer != "" {
		t.Fatalf("answer = %q, want empty (no fabrication in delegation)", got.Answer)
	}
	if len(got.Sources) == 0 {
		t.Fatalf("sources empty, want the F-0001 hit even without a provider")
	}
}

// TestAskEndpointEmptyQuery: a blank question is a 400 with a pt-BR message.
func TestAskEndpointEmptyQuery(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, "proj-ask"), http.MethodPost, "/api/ask", `{"query":"  "}`, &got)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] == "" {
		t.Fatalf("body = %v, want pt-BR error message", got)
	}
}
