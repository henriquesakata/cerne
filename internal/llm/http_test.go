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

// sseChatServer is an httptest server speaking the Messages API streaming wire
// format: it records the request body and replies with the given SSE events.
func sseChatServer(t *testing.T, gotBody *map[string]any, events []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotBody != nil {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, gotBody)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			_, _ = io.WriteString(w, ev+"\n\n")
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newStreamProvider(t *testing.T, baseURL string) *httpProvider {
	t.Helper()
	t.Setenv("TEST_LLM_KEY", "sk")
	prov, err := NewProvider(project.LLMConfig{Mode: "byo-key", KeyEnv: "TEST_LLM_KEY", BaseURL: baseURL})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return prov.(*httpProvider)
}

// TestHTTPProviderStream (F-0240 criterion 1): two content_block_delta events
// reach onDelta in order, the return is their concatenation, and the request
// asks the API for streaming.
func TestHTTPProviderStream(t *testing.T) {
	var body map[string]any
	srv := sseChatServer(t, &body, []string{
		`event: message_start` + "\n" + `data: {"type":"message_start"}`,
		`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"parte um. "}}`,
		`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"parte dois."}}`,
		`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
	})

	var deltas []string
	got, err := newStreamProvider(t, srv.URL).Stream(context.Background(), "p", func(d string) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got != "parte um. parte dois." {
		t.Fatalf("Stream = %q, want the concatenation", got)
	}
	if len(deltas) != 2 || deltas[0] != "parte um. " || deltas[1] != "parte dois." {
		t.Fatalf("deltas = %#v", deltas)
	}
	if body["stream"] != true {
		t.Fatalf("request body.stream = %v, want true", body["stream"])
	}
}

// TestHTTPProviderStreamAPIError (F-0240 criterion 8): an error event after the
// first delta surfaces as an error, with the delta already delivered.
func TestHTTPProviderStreamAPIError(t *testing.T) {
	srv := sseChatServer(t, nil, []string{
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"começo"}}`,
		`data: {"type":"error","error":{"message":"overloaded"}}`,
	})

	var deltas []string
	_, err := newStreamProvider(t, srv.URL).Stream(context.Background(), "p", func(d string) error {
		deltas = append(deltas, d)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "overloaded") {
		t.Fatalf("err = %v, want the API error message", err)
	}
	if len(deltas) != 1 || deltas[0] != "começo" {
		t.Fatalf("deltas = %#v, want the partial delivered before the error", deltas)
	}
}

// TestHTTPProviderStreamOnDeltaAbort: a callback error aborts the stream and
// is returned as-is.
func TestHTTPProviderStreamOnDeltaAbort(t *testing.T) {
	srv := sseChatServer(t, nil, []string{
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"a"}}`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"b"}}`,
	})

	boom := errors.New("client gone")
	calls := 0
	_, err := newStreamProvider(t, srv.URL).Stream(context.Background(), "p", func(string) error {
		calls++
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the callback error", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (aborted on first)", calls)
	}
}

// TestHTTPProviderStreamNon2xx mirrors Complete's error contract.
func TestHTTPProviderStreamNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	t.Cleanup(srv.Close)

	_, err := newStreamProvider(t, srv.URL).Stream(context.Background(), "p", func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v, want a 429 status error", err)
	}
}
