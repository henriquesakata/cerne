package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/project"
)

// TestHTTPProviderComplete drives the provider against an httptest server and
// asserts the wire format: the three required headers, the model, the user
// message, and that no sampling/thinking knobs are sent. The server returns a
// canned text block and Complete returns its concatenated text.
func TestHTTPProviderComplete(t *testing.T) {
	var gotPath, gotKey, gotVersion, gotContentType string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		gotContentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"resposta sintetizada"}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_LLM_KEY", "sk-test-123")
	cfg := project.LLMConfig{Mode: "byo-key", Provider: "anthropic",
		Model: "claude-opus-4-8", KeyEnv: "TEST_LLM_KEY", BaseURL: srv.URL, MaxTokens: 4321}
	prov, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	got, err := prov.Complete(context.Background(), "prompt do compile")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "resposta sintetizada" {
		t.Fatalf("Complete = %q, want %q", got, "resposta sintetizada")
	}

	if gotPath != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", gotPath)
	}
	if gotKey != "sk-test-123" {
		t.Fatalf("x-api-key = %q, want sk-test-123", gotKey)
	}
	if gotVersion != "2023-06-01" {
		t.Fatalf("anthropic-version = %q, want 2023-06-01", gotVersion)
	}
	if !strings.HasPrefix(gotContentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", gotContentType)
	}
	if body["model"] != "claude-opus-4-8" {
		t.Fatalf("body.model = %v, want claude-opus-4-8", body["model"])
	}
	if mt, ok := body["max_tokens"].(float64); !ok || int(mt) != 4321 {
		t.Fatalf("body.max_tokens = %v, want 4321", body["max_tokens"])
	}
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("body.messages = %v, want one message", body["messages"])
	}
	m0 := msgs[0].(map[string]any)
	if m0["role"] != "user" || m0["content"] != "prompt do compile" {
		t.Fatalf("message = %v, want user/prompt do compile", m0)
	}
	// Sampling/thinking knobs 400 on the default model — they must be absent.
	for _, banned := range []string{"temperature", "top_p", "top_k", "thinking"} {
		if _, present := body[banned]; present {
			t.Fatalf("request must not send %q, body = %v", banned, body)
		}
	}
}

// TestHTTPProviderJoinsTextBlocks concatenates the text of every text-type block
// and ignores non-text blocks.
func TestHTTPProviderJoinsTextBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"parte um. "},{"type":"thinking","thinking":"ignore"},{"type":"text","text":"parte dois."}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_LLM_KEY", "sk")
	prov, err := NewProvider(project.LLMConfig{Mode: "byo-key", KeyEnv: "TEST_LLM_KEY", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	got, err := prov.Complete(context.Background(), "p")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "parte um. parte dois." {
		t.Fatalf("Complete = %q, want %q", got, "parte um. parte dois.")
	}
}

// TestHTTPProviderNon200 surfaces the status code and a slice of the body.
func TestHTTPProviderNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad model"}}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_LLM_KEY", "sk")
	prov, err := NewProvider(project.LLMConfig{Mode: "byo-key", KeyEnv: "TEST_LLM_KEY", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	_, err = prov.Complete(context.Background(), "p")
	if err == nil {
		t.Fatalf("Complete on 400 succeeded, want error")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("error %q does not mention status 400", err)
	}
	if !strings.Contains(err.Error(), "bad model") {
		t.Fatalf("error %q does not include a slice of the body", err)
	}
}

// TestNewProviderMissingKey errors with ErrAPIKeyMissing naming the env var.
func TestNewProviderMissingKey(t *testing.T) {
	t.Setenv("TEST_LLM_KEY", "")
	_, err := NewProvider(project.LLMConfig{Mode: "byo-key", KeyEnv: "TEST_LLM_KEY"})
	if !errors.Is(err, ErrAPIKeyMissing) {
		t.Fatalf("err = %v, want ErrAPIKeyMissing", err)
	}
	if !strings.Contains(err.Error(), "TEST_LLM_KEY") {
		t.Fatalf("error %q does not name the env var", err)
	}
}

// TestNewProviderUnsupportedProvider errors naming the unsupported provider.
func TestNewProviderUnsupportedProvider(t *testing.T) {
	t.Setenv("TEST_LLM_KEY", "sk")
	_, err := NewProvider(project.LLMConfig{Mode: "byo-key", Provider: "openai", KeyEnv: "TEST_LLM_KEY"})
	if err == nil {
		t.Fatalf("NewProvider with provider=openai succeeded, want error")
	}
	if !strings.Contains(err.Error(), "openai") {
		t.Fatalf("error %q does not name the unsupported provider", err)
	}
}

// TestNewProviderDelegationIsMisuse returns ErrNoProviderForDelegation so a
// caller that wrongly asks for a Provider in delegation mode can detect it.
func TestNewProviderDelegationIsMisuse(t *testing.T) {
	for _, mode := range []string{"", "delegation"} {
		_, err := NewProvider(project.LLMConfig{Mode: mode})
		if !errors.Is(err, ErrNoProviderForDelegation) {
			t.Fatalf("NewProvider(mode=%q) err = %v, want ErrNoProviderForDelegation", mode, err)
		}
	}
}

// TestNewProviderAppliesDefaults builds a provider with only mode+key set; the
// default base_url/model/provider fill in, and the request reaches the server
// at /v1/messages with the default model.
func TestNewProviderAppliesDefaults(t *testing.T) {
	var gotModel any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		gotModel = body["model"]
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_LLM_KEY", "sk")
	// base_url overridden to the test server, but model/provider left to defaults.
	prov, err := NewProvider(project.LLMConfig{Mode: "byo-key", KeyEnv: "TEST_LLM_KEY", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if _, err := prov.Complete(context.Background(), "p"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotModel != "claude-opus-4-8" {
		t.Fatalf("default model = %v, want claude-opus-4-8", gotModel)
	}
}
