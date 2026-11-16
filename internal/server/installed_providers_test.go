package server

import (
	"bytes"
	"net/http"
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
	t.Helper()
	var seen *project.LLMConfig
	orig := newAgentProvider
	t.Cleanup(func() { newAgentProvider = orig })
	newAgentProvider = func(cfg project.LLMConfig, _ string, _ bool) (llm.Provider, error) {
		seen = &cfg
		return &llm.Fake{Reply: "ok"}, nil
	}
	body := `{"messages":[{"role":"user","content":"Oi"}],"model":"gpt-x","effort":"high","provider":"` + provider + `"}`
	rec := doJSON(t, New(repoDir, projDir, "proj-chat"), http.MethodPost, "/api/chat", body, &struct{}{})
	return seen, rec.Code, rec.Body.String()
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
