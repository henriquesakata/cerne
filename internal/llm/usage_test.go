package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// TestClaudeCLIReportsUsage: CompleteUsage runs the one-shot claude -p in JSON,
// returns the result as the reply with the tokens of every model it used; a
// plain-text answer reports nothing; Complete keeps the text argv.
func TestClaudeCLIReportsUsage(t *testing.T) { // F-0502 crit. 10
	out := `{"type":"result","is_error":false,"result":" ok ","modelUsage":{` +
		`"claude-haiku-5":{"inputTokens":10,"outputTokens":3,"cacheReadInputTokens":5,"cacheCreationInputTokens":2},` +
		`"claude-opus-5-5":{"inputTokens":100,"outputTokens":40}}}`
	var args []string
	p := &cliProvider{bin: "claude", run: func(_ context.Context, _, _ string, a []string, _ string) (string, string, error) {
		args = a
		return out, "", nil
	}}
	if _, err := p.Complete(t.Context(), "q"); err != nil || slices.Contains(args, "json") {
		t.Fatalf("Complete: argv %v, err %v; want text", args, err)
	}

	got, u, err := p.CompleteUsage(t.Context(), "q")
	if err != nil || got != "ok" || !slices.Contains(args, "json") {
		t.Fatalf("reply = %q, %v, argv %v", got, err, args)
	}
	if want := (Usage{Model: "claude-opus-5-5", InputTokens: 117, OutputTokens: 43}); u != want {
		t.Errorf("usage = %+v, want %+v", u, want)
	}

	plain := &cliProvider{bin: "claude", run: func(context.Context, string, string, []string, string) (string, string, error) {
		return "so texto", "", nil
	}}
	if got, u, _ := plain.CompleteUsage(t.Context(), "q"); got != "so texto" || u != (Usage{}) {
		t.Errorf("plain = %q, usage %+v", got, u)
	}
}

// TestHTTPProviderReportsUsage: the Anthropic Messages response carries the
// tokens of the call; CompleteUsage returns them with the reply.
func TestHTTPProviderReportsUsage(t *testing.T) { // F-0502 crit. 10
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"model":"claude-x","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":21,"output_tokens":8}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_LLM_KEY", "sk-test")
	prov, err := NewProvider(project.LLMConfig{Mode: "byo-key", Provider: "anthropic", Model: "claude-x", KeyEnv: "TEST_LLM_KEY", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := prov.(UsageCompleter)
	if !ok {
		t.Fatalf("%T does not report usage", prov)
	}
	reply, u, err := c.CompleteUsage(t.Context(), "q")
	if err != nil || reply != "ok" {
		t.Fatalf("reply = %q, %v", reply, err)
	}
	if want := (Usage{Model: "claude-x", InputTokens: 21, OutputTokens: 8}); u != want {
		t.Errorf("usage = %+v, want %+v", u, want)
	}
}

// TestCompleteUsageSharesAProviderSafely: the usage travels with the call, not
// in the provider, so concurrent calls on one provider each get their own
// (run with -race).
func TestCompleteUsageSharesAProviderSafely(t *testing.T) { // F-0502 crit. 10
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, `{"model":"m","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":5,"output_tokens":2}}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_LLM_KEY", "sk-test")
	prov, err := NewProvider(project.LLMConfig{Mode: "byo-key", Provider: "anthropic", Model: "m", KeyEnv: "TEST_LLM_KEY", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				_, _, _ = prov.(UsageCompleter).CompleteUsage(t.Context(), "q")
			} else {
				_, _ = prov.Complete(t.Context(), "q")
			}
		}()
	}
	wg.Wait()
}
