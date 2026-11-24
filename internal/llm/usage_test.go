package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

// TestClaudeCLIReportsUsage: once usage is asked for, the one-shot claude -p
// runs in JSON, returns the result as the reply and keeps the tokens of every
// model it used; a plain-text answer reports nothing; without the request the
// argv stays text.
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
		t.Fatalf("without WantUsage: argv %v, err %v; want text", args, err)
	}
	if got := p.LastUsage(); got != (Usage{}) {
		t.Errorf("usage without a request = %+v", got)
	}

	WantUsage(p)
	got, err := p.Complete(t.Context(), "q")
	if err != nil || got != "ok" || !slices.Contains(args, "json") {
		t.Fatalf("reply = %q, %v, argv %v", got, err, args)
	}
	want := Usage{Model: "claude-opus-5-5", InputTokens: 117, OutputTokens: 43}
	if u := p.LastUsage(); u != want {
		t.Errorf("usage = %+v, want %+v", u, want)
	}

	plain := &cliProvider{bin: "claude", wantUsage: true, run: func(context.Context, string, string, []string, string) (string, string, error) {
		return "so texto", "", nil
	}}
	if got, _ := plain.Complete(t.Context(), "q"); got != "so texto" || plain.LastUsage() != (Usage{}) {
		t.Errorf("plain = %q, usage %+v", got, plain.LastUsage())
	}
}

// TestHTTPProviderReportsUsage: the Anthropic Messages response carries the
// tokens of the call; LastUsage returns them.
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
	if _, err := prov.Complete(t.Context(), "q"); err != nil {
		t.Fatal(err)
	}
	r, ok := prov.(UsageReporter)
	if !ok {
		t.Fatalf("%T does not report usage", prov)
	}
	if want := (Usage{Model: "claude-x", InputTokens: 21, OutputTokens: 8}); r.LastUsage() != want {
		t.Errorf("usage = %+v, want %+v", r.LastUsage(), want)
	}
}
