package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// keyServer answers a chat completion and records the Authorization header.
func keyServer(t *testing.T, auth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*auth = r.Header.Get("Authorization")
		w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func clearKeys(t *testing.T) {
	t.Helper()
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	for _, v := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY", "CERNE_COMPATIBLE_API_KEY", "CERNE_LLM_API_KEY"} {
		t.Setenv(v, "")
	}
}

// F-0414 (B2): each provider reads its own key. The legacy shared name is kept
// for anthropic and openai only; a compatible endpoint never sees it.
func TestCompatibleNeverGetsAnotherProvidersKey(t *testing.T) {
	clearKeys(t)
	var auth string
	srv := keyServer(t, &auth)
	t.Setenv("CERNE_LLM_API_KEY", "sk-anthropic-legacy")
	t.Setenv("ANTHROPIC_API_KEY", "sk-anthropic")
	t.Setenv("OPENAI_API_KEY", "sk-openai")
	prov, err := NewProvider(project.LLMConfig{Mode: "byo-key", Provider: "compatible", Model: "m", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prov.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		t.Fatalf("a compatible endpoint received %q", auth)
	}
	t.Setenv("CERNE_COMPATIBLE_API_KEY", "sk-groq")
	prov, _ = NewProvider(project.LLMConfig{Mode: "byo-key", Provider: "compatible", Model: "m", BaseURL: srv.URL})
	prov.Complete(context.Background(), "q")
	if auth != "Bearer sk-groq" {
		t.Fatalf("compatible's own variable: auth = %q", auth)
	}
}

func TestProviderKeyResolutionOrder(t *testing.T) {
	clearKeys(t)
	cases := []struct {
		name, provider   string
		env              map[string]string
		wantKey, wantEnv string
	}{
		{"anthropic own", "anthropic", map[string]string{"ANTHROPIC_API_KEY": "a", "CERNE_LLM_API_KEY": "legacy"}, "a", "ANTHROPIC_API_KEY"},
		{"anthropic legacy", "anthropic", map[string]string{"CERNE_LLM_API_KEY": "legacy"}, "legacy", "CERNE_LLM_API_KEY"},
		{"openai legacy", "openai", map[string]string{"CERNE_LLM_API_KEY": "legacy"}, "legacy", "CERNE_LLM_API_KEY"},
		{"openai own wins", "openai", map[string]string{"OPENAI_API_KEY": "o", "CERNE_LLM_API_KEY": "legacy"}, "o", "OPENAI_API_KEY"},
		{"gemini no legacy", "gemini", map[string]string{"CERNE_LLM_API_KEY": "legacy"}, "", "GEMINI_API_KEY"},
		{"openrouter no legacy", "openrouter", map[string]string{"CERNE_LLM_API_KEY": "legacy"}, "", "OPENROUTER_API_KEY"},
		{"compatible no legacy", "compatible", map[string]string{"CERNE_LLM_API_KEY": "legacy", "OPENAI_API_KEY": "o"}, "", "CERNE_COMPATIBLE_API_KEY"},
	}
	for _, c := range cases {
		for k, v := range c.env {
			t.Setenv(k, v)
		}
		got, _, env, err := project.LLMConfig{Provider: c.provider}.ResolveKey()
		if err != nil || got != c.wantKey || env != c.wantEnv {
			t.Errorf("%s: key=%q env=%q err=%v, want %q %q", c.name, got, env, err, c.wantKey, c.wantEnv)
		}
		for k := range c.env {
			t.Setenv(k, "")
		}
	}
	// An explicit key_env is exclusive.
	t.Setenv("ANTHROPIC_API_KEY", "a")
	t.Setenv("MY_KEY", "mine")
	if got, _, env, _ := (project.LLMConfig{KeyEnv: "MY_KEY"}).ResolveKey(); got != "mine" || env != "MY_KEY" {
		t.Errorf("explicit key_env: %q %q", got, env)
	}
	if got, _, _, _ := (project.LLMConfig{KeyEnv: "UNSET_KEY_VAR"}).ResolveKey(); got != "" {
		t.Errorf("explicit key_env must not fall through, got %q", got)
	}
	// The credentials file is read under the provider's own name.
	if err := project.WriteCredential("OPENAI_API_KEY", "from-file"); err != nil {
		t.Fatal(err)
	}
	if got, src, env, _ := (project.LLMConfig{Provider: "openai"}).ResolveKey(); got != "from-file" || src != "file" || env != "OPENAI_API_KEY" {
		t.Errorf("file key: %q %q %q", got, src, env)
	}
}

// F-0414 (B3): a model chosen for a byo-key provider never reaches a CLI.
func TestCLIModelOnlyWithoutAProvider(t *testing.T) {
	for provider, want := range map[string]string{"": "gpt-5", "openai": "", "compatible": ""} {
		prov, err := NewProvider(project.LLMConfig{Mode: ModeCodexCLI, Provider: provider, Model: "gpt-5"})
		if err != nil {
			t.Fatal(err)
		}
		if got := prov.(*cliProvider).model; got != want {
			t.Errorf("provider %q: codex model = %q, want %q", provider, got, want)
		}
	}
}

// B-0092: with an Anthropic key in the shared legacy variable, the openai
// provider is not built — there is no call that could carry the key.
func TestOpenAIProviderNeverBuiltWithAnAnthropicKey(t *testing.T) {
	clearKeys(t)
	t.Setenv("CERNE_LLM_API_KEY", "sk-ant-api03-SECRETSECRET")
	_, err := NewProvider(project.LLMConfig{Mode: "byo-key", Provider: "openai", Model: "gpt-test"})
	if !errors.Is(err, ErrAPIKeyMissing) || !strings.Contains(err.Error(), "OPENAI_API_KEY") || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v, want ErrAPIKeyMissing naming OPENAI_API_KEY and no key text", err)
	}
	t.Setenv("OPENAI_API_KEY", "sk-ant-api03-SECRETSECRET")
	if _, err := NewProvider(project.LLMConfig{Mode: "byo-key", Provider: "openai", Model: "gpt-test"}); !errors.Is(err, project.ErrKeyWrongVendor) {
		t.Fatalf("err = %v, want ErrKeyWrongVendor for an Anthropic key under OPENAI_API_KEY", err)
	}
	if _, err := NewProvider(project.LLMConfig{Mode: "byo-key", Provider: "anthropic", Model: "m"}); err != nil {
		t.Fatalf("anthropic with its key in the legacy variable: %v", err)
	}
}
