package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/project"
)

// stubInstalledCLIs makes the PATH probe report exactly these subscription CLIs.
func stubInstalledCLIs(t *testing.T, found ...string) {
	t.Helper()
	orig := project.FoundSubscriptionCLIs
	t.Cleanup(func() { project.FoundSubscriptionCLIs = orig })
	project.FoundSubscriptionCLIs = func() []string { return found }
}

// TestLLMConfigListsInstalledCLIs (F-0440 criterion 1): the configured mode's
// vendor first, then one per installed subscription CLI, never repeated; the
// remote serve keeps only the configured one.
func TestLLMConfigListsInstalledCLIs(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	stubInstalledCLIs(t, "claude", "codex", "cursor-agent")
	cfg := project.LLMConfig{Mode: "claude-cli"}
	if got := strings.Join(llmConfig(cfg, false).Providers, ","); got != "anthropic,openai,cursor" {
		t.Fatalf("local providers = %q, want anthropic,openai,cursor", got)
	}
	if got := strings.Join(llmConfig(cfg, true).Providers, ","); got != "anthropic" {
		t.Fatalf("remote providers = %q, want anthropic", got)
	}
	// byo-key with an installed CLI of the same vendor: no duplicate.
	if got := strings.Join(llmConfig(project.LLMConfig{Mode: "byo-key"}, false).Providers, ","); got != "anthropic,openai,cursor" {
		t.Fatalf("byo-key providers = %q, want anthropic,openai,cursor", got)
	}
}

// TestPreferredProviderAcceptsInstalledCLI (criterion 5): the PUT takes an
// installed CLI's vendor and leaves .cernerc byte for byte as it was.
func TestPreferredProviderAcceptsInstalledCLI(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	stubInstalledCLIs(t, "claude", "codex")
	repoDir, projDir := newBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "p", Backend: "b", LLM: project.LLMConfig{Mode: "claude-cli"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(projDir, ".cernerc"))
	h := New(repoDir, projDir, "")
	if rec := doJSON(t, h, http.MethodPut, "/api/config/models/provider", `{"provider":"openai"}`, new(map[string]any)); rec.Code != http.StatusOK {
		t.Fatalf("installed codex: status = %d; body: %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, h, http.MethodPut, "/api/config/models/provider", `{"provider":"gemini"}`, new(map[string]any)); rec.Code != http.StatusBadRequest {
		t.Fatalf("not installed gemini: status = %d, want 400", rec.Code)
	}
	after, _ := os.ReadFile(filepath.Join(projDir, ".cernerc"))
	if !bytes.Equal(before, after) {
		t.Fatalf(".cernerc changed:\n%s\n---\n%s", before, after)
	}
}

// providerTurn posts one chat turn with the given provider and reports the
// config the agent seam saw (nil when it was never called) and the response.
func providerTurn(t *testing.T, projDir, repoDir, provider string) (*project.LLMConfig, int, string) {
	cfg, _, code, body := providerTurnDir(t, projDir, repoDir, provider)
	return cfg, code, body
}

// providerTurnDir is providerTurn plus the directory the agent seam was given.
func providerTurnDir(t *testing.T, projDir, repoDir, provider string) (*project.LLMConfig, string, int, string) {
	t.Helper()
	var seen *project.LLMConfig
	orig := newAgentProvider
	t.Cleanup(func() { newAgentProvider = orig })
	var dir string
	newAgentProvider = func(cfg project.LLMConfig, d string, _ bool) (llm.Provider, error) {
		seen, dir = &cfg, d
		return &llm.Fake{Reply: "ok"}, nil
	}
	body := `{"messages":[{"role":"user","content":"Oi"}],"model":"gpt-x","effort":"high","provider":"` + provider + `"}`
	rec := doJSON(t, New(repoDir, projDir, "proj-chat"), http.MethodPost, "/api/chat", body, &struct{}{})
	return seen, dir, rec.Code, rec.Body.String()
}

// TestChatProviderSwitchesMode (F-0440 criteria 2, 3): the picked installed
// CLI's mode, model and effort reach the turn's provider; the project's own
// config is untouched; an unreachable provider is a 400 that names it, with no
// provider built.
func TestChatProviderSwitchesMode(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	stubInstalledCLIs(t, "claude", "codex")
	repoDir, projDir, _ := newGitBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "proj-chat", Backend: "unused", LLM: project.LLMConfig{Mode: "claude-cli"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(projDir, ".cernerc"))

	if cfg, code, body := providerTurn(t, projDir, repoDir, "openai"); code != http.StatusOK || cfg == nil || cfg.Mode != "codex-cli" || cfg.Model != "gpt-x" || cfg.Effort != "high" {
		t.Fatalf("openai turn = %d %+v; body: %s", code, cfg, body)
	}
	for _, same := range []string{"", "anthropic"} {
		if cfg, code, _ := providerTurn(t, projDir, repoDir, same); code != http.StatusOK || cfg == nil || cfg.Mode != "claude-cli" {
			t.Fatalf("provider %q turn = %d %+v, want the configured claude-cli", same, code, cfg)
		}
	}
	cfg, code, body := providerTurn(t, projDir, repoDir, "gemini")
	if code != http.StatusBadRequest || cfg != nil || !strings.Contains(body, "gemini") {
		t.Fatalf("gemini (not installed) = %d, seam called=%v; body: %s", code, cfg != nil, body)
	}
	if after, _ := os.ReadFile(filepath.Join(projDir, ".cernerc")); !bytes.Equal(before, after) {
		t.Fatalf(".cernerc changed by a chat turn")
	}
}

// TestChatAuthorUsesTurnProvider (criterion 4): the spec the chat proposes is
// credited to the CLI that answered and the model it was given.
func TestChatAuthorUsesTurnProvider(t *testing.T) {
	agentServerEnv(t)
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	stubInstalledCLIs(t, "claude", "codex")
	repoDir, projDir, _ := newGitBackend(t)
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte("backend = \"x\"\n\n[llm]\nmode = \"claude-cli\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := "# Exportar CSV\\n\\n## Problema\\nFalta.\\n\\n## Objetivo e critérios de aceitação\\n\\nX.\\n\\n## Decisões do design\\n\\nX.\\n\\n## Desenho\\n\\nX.\\n\\n## Verificação\\n\\nX.\\n\\n## Execução recomendada (agente)\\n\\nX.\\n\\n## Fora de escopo\\n\\nX.\\n"
	rec := doJSON(t, New(repoDir, projDir, "proj-chat-author"), http.MethodPost, "/api/chat/register",
		`{"title":"Exportar CSV","provider":"openai","model":"gpt-x","spec_body":"`+spec+`"}`, &map[string]any{})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d %s", rec.Code, rec.Body.String())
	}
	if p := lastDocEdited(t, repoDir, "F-0002"); p["provider"] != "codex-cli" || p["model"] != "gpt-x" {
		t.Fatalf("author = %v, want codex-cli/gpt-x", p)
	}
}

// TestLLMConfigServesCodexModelsWhenInstalled (criterion 3): picking OpenAI in a
// claude-cli project needs the Codex catalog in the options; the remote serve
// and a machine without codex do not get it.
func TestLLMConfigServesCodexModelsWhenInstalled(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	cache := `{"models":[{"slug":"gpt-x","display_name":"GPT X","visibility":"list","priority":1,
		"default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"}]}]}`
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(cache), 0o600); err != nil {
		t.Fatal(err)
	}
	has := func(r llmConfigResponse) bool {
		for _, e := range r.Models {
			if e.ID == "gpt-x" {
				return true
			}
		}
		return false
	}
	cfg := project.LLMConfig{Mode: "claude-cli"}
	stubInstalledCLIs(t, "claude", "codex")
	if !has(llmConfig(cfg, false)) || has(llmConfig(cfg, true)) {
		t.Fatalf("codex installed: local must serve its models, remote must not")
	}
	stubInstalledCLIs(t, "claude")
	if has(llmConfig(cfg, false)) {
		t.Fatalf("codex not installed: its models must not be served")
	}
}

func claudeProject(t *testing.T, mode string, extra string) (repoDir, projDir string) {
	t.Helper()
	repoDir, projDir, _ = newGitBackend(t)
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte("backend = \"x\"\n\n[llm]\nmode = \""+mode+"\"\n"+extra), 0o644); err != nil {
		t.Fatal(err)
	}
	return repoDir, projDir
}

// TestChatSwitchedTurnGetsProjectDirAndCodexMode (F-0440 criterion 3, PR #20):
// a claude-cli project asked for openai hands the seam the project dir and the
// codex mode — what sets the project-files MCP up — and a project effort never
// leaks into the other vendor's turn.
func TestChatSwitchedTurnGetsProjectDirAndCodexMode(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	stubInstalledCLIs(t, "claude", "codex")
	repoDir, projDir := claudeProject(t, "claude-cli", "effort = \"max\"\n")
	body := `{"messages":[{"role":"user","content":"Oi"}],"provider":"openai"}`
	var seen project.LLMConfig
	var dir string
	orig := newAgentProvider
	t.Cleanup(func() { newAgentProvider = orig })
	newAgentProvider = func(cfg project.LLMConfig, d string, _ bool) (llm.Provider, error) {
		seen, dir = cfg, d
		return &llm.Fake{Reply: "ok"}, nil
	}
	if rec := doJSON(t, New(repoDir, projDir, "p"), http.MethodPost, "/api/chat", body, &struct{}{}); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if dir != projDir || seen.Mode != "codex-cli" || seen.Effort != "" {
		t.Fatalf("seam got dir=%q cfg=%+v, want the project dir, codex-cli, no effort", dir, seen)
	}
}

// TestLLMConfigConfiguredFirstAndKeylessStaysOut (criterion 1): the configured
// vendor leads whatever the PATH order says, and a byo-key vendor without a
// key is never listed nor accepted because claude happens to be installed.
func TestLLMConfigConfiguredFirstAndKeylessStaysOut(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	stubInstalledCLIs(t, "claude", "codex")
	if got := strings.Join(llmConfig(project.LLMConfig{Mode: "codex-cli"}, false).Providers, ","); got != "openai,anthropic" {
		t.Fatalf("codex project providers = %q, want openai,anthropic", got)
	}
	stubInstalledCLIs(t, "claude")
	cfg := project.LLMConfig{Mode: "claude-cli"}
	if got := strings.Join(llmConfig(cfg, false).Providers, ","); got != "anthropic" {
		t.Fatalf("providers = %q, want anthropic only", got)
	}
	repoDir, projDir := claudeProject(t, "claude-cli", "")
	if c, code, body := providerTurn(t, projDir, repoDir, "openrouter"); code != http.StatusBadRequest || c != nil || !strings.Contains(body, "openrouter") {
		t.Fatalf("keyless openrouter = %d, seam called=%v: %s", code, c != nil, body)
	}
}

// TestChatRegisterRemoteNeverCreditsLocalOnlyProvider (criteria 2, 4): the
// remote serve refuses /api/chat for an installed-CLI provider, so its register
// never credits one either.
func TestChatRegisterRemoteNeverCreditsLocalOnlyProvider(t *testing.T) {
	agentServerEnv(t)
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	stubInstalledCLIs(t, "claude", "codex")
	repoDir, projDir := claudeProject(t, "claude-cli", "")
	spec := "# Exportar CSV\\n\\n## Problema\\nFalta.\\n\\n## Objetivo e critérios de aceitação\\n\\nX.\\n\\n## Decisões do design\\n\\nX.\\n\\n## Desenho\\n\\nX.\\n\\n## Verificação\\n\\nX.\\n\\n## Execução recomendada (agente)\\n\\nX.\\n\\n## Fora de escopo\\n\\nX.\\n"
	req := httptest.NewRequest(http.MethodPost, "/api/chat/register", strings.NewReader(`{"title":"Exportar CSV","provider":"openai","model":"gpt-x","spec_body":"`+spec+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), remoteKey{}, true))
	rec := httptest.NewRecorder()
	handleChatRegister(rec, req, repoDir, projDir)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d %s", rec.Code, rec.Body.String())
	}
	if p := lastDocEdited(t, repoDir, "F-0002"); p["provider"] != "claude-cli" {
		t.Fatalf("remote author = %v, want the configured claude-cli", p)
	}
}

// TestLLMConfigCodexModelsNotDuplicated (criterion 3): a byo-key openai project
// whose catalog already lists a model the Codex cache also lists shows it once.
func TestLLMConfigCodexModelsNotDuplicated(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CERNE_CONFIG_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "models.json"), []byte(`{"entries":[{"id":"gpt-x","label":"GPT X","provider":"openai","visibility":"current"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	cache := `{"models":[{"slug":"gpt-x","display_name":"GPT X","visibility":"list","priority":1}]}`
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(cache), 0o600); err != nil {
		t.Fatal(err)
	}
	stubInstalledCLIs(t, "codex")
	n := 0
	for _, e := range llmConfig(project.LLMConfig{Mode: "byo-key", Provider: "openai"}, false).Models {
		if e.ID == "gpt-x" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("gpt-x listed %d times, want once", n)
	}
}

// TestPreferredProviderRefusalNamesInstalledCLIs (criterion 5): the 400 says
// what is accepted — configured or an installed subscription CLI's vendor.
func TestPreferredProviderRefusalNamesInstalledCLIs(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	stubInstalledCLIs(t, "claude")
	repoDir, projDir := claudeProject(t, "claude-cli", "")
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodPut, "/api/config/models/provider", `{"provider":"gemini"}`, new(map[string]any))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "installed") {
		t.Fatalf("refusal = %d %s, want a 400 naming installed CLIs", rec.Code, rec.Body.String())
	}
}

type streamFake struct{ llm.Fake }

func (streamFake) Stream(_ context.Context, _ string, onDelta func(string) error) (string, error) {
	_ = onDelta("o")
	return "ok", nil
}

// TestChatReplyNamesWhoAnswered (F-0440 criterion 4): the single-shot reply, the
// streaming terminal event and the tool turn each carry the provider, model and
// effort the turn really ran on after a switch — not an echo of the request.
func TestChatReplyNamesWhoAnswered(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	stubInstalledCLIs(t, "claude", "codex")
	repoDir, projDir := claudeProject(t, "claude-cli", "")
	orig := newAgentProvider
	t.Cleanup(func() { newAgentProvider = orig })
	last := func(body string) struct {
		By *struct{ Provider, Model, Effort string } `json:"answered_by"`
	} {
		var got struct {
			By *struct{ Provider, Model, Effort string } `json:"answered_by"`
		}
		lines := strings.Split(strings.TrimSpace(body), "\n")
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &got); err != nil {
			t.Fatalf("last line %q: %v", lines[len(lines)-1], err)
		}
		return got
	}
	check := func(name, body string) {
		t.Helper()
		by := last(body).By
		if by == nil || by.Provider != "openai" || by.Model != "gpt-x" || by.Effort != "high" {
			t.Fatalf("%s: answered_by = %+v; body: %s", name, by, body)
		}
	}
	post := func(body string) string {
		req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		New(repoDir, projDir, "p").ServeHTTP(rec, req)
		return rec.Body.String()
	}
	turn := `{"messages":[{"role":"user","content":"Oi"}],"provider":"openai","model":"gpt-x","effort":"high"}`
	newAgentProvider = func(project.LLMConfig, string, bool) (llm.Provider, error) { return &llm.Fake{Reply: "ok"}, nil }
	check("single-shot", post(turn))
	newAgentProvider = func(project.LLMConfig, string, bool) (llm.Provider, error) { return &streamFake{}, nil }
	check("streaming", post(turn))
	// the configured vendor answers as itself: claude-cli names no model.
	if by := last(post(`{"messages":[{"role":"user","content":"Oi"}],"model":"claude-opus-5-5"}`)).By; by == nil || by.Provider != "anthropic" || by.Model != "" {
		t.Fatalf("configured turn answered_by = %+v", by)
	}
	setTree(t, fixtureTree)
	newAgentProvider = func(project.LLMConfig, string, bool) (llm.Provider, error) { return &toolFake{}, nil }
	check("tool turn", post(`{"messages":[`+confirmedTranscript+`],"conversation_id":"c1","confirm_turn":1,"confirm":true,"provider":"openai","model":"gpt-x","effort":"high"}`))
}

type reportingFake struct {
	llm.Fake
	model string
}

func (r *reportingFake) AnsweredModel() string { return r.model }

// TestChatClaudeReplyNamesTheModelTheCLIReported (F-0440 criterion 4): a
// claude-cli turn names the model the CLI reported it ran, not the composer's
// chip (claude is never passed --model) and no effort it was never given.
func TestChatClaudeReplyNamesTheModelTheCLIReported(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	stubInstalledCLIs(t, "claude")
	repoDir, projDir := claudeProject(t, "claude-cli", "")
	orig := newAgentProvider
	t.Cleanup(func() { newAgentProvider = orig })
	newAgentProvider = func(project.LLMConfig, string, bool) (llm.Provider, error) {
		return &reportingFake{llm.Fake{Reply: "ok"}, "claude-opus-5-5"}, nil
	}
	var got struct {
		By *struct{ Provider, Model, Effort string } `json:"answered_by"`
	}
	doJSON(t, New(repoDir, projDir, "p"), http.MethodPost, "/api/chat", `{"messages":[{"role":"user","content":"Oi"}],"model":"claude-sonnet-5-5","effort":"medium"}`, &got)
	if got.By == nil || got.By.Provider != "anthropic" || got.By.Model != "claude-opus-5-5" || got.By.Effort != "" {
		t.Fatalf("answered_by = %+v, want anthropic / the reported model / no effort", got.By)
	}
}
