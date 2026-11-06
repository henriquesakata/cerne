package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitlab.com/rockt/cerne/internal/project"
)

var _ ToolCaller = (*openaiProvider)(nil)

// openaiServer replies to each chat completion with the next canned body (the
// last repeats), recording path, auth and every request body.
func openaiServer(t *testing.T, bodies *[]map[string]any, auth *string, replies ...string) *httptest.Server {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		*auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		var b map[string]any
		_ = json.Unmarshal(raw, &b)
		*bodies = append(*bodies, b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, replies[min(n, len(replies)-1)])
		n++
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newOpenAI(t *testing.T, baseURL string) *openaiProvider {
	t.Helper()
	t.Setenv("TEST_LLM_KEY", "sk-oa")
	prov, err := NewProvider(project.LLMConfig{Mode: "byo-key", Provider: "openai", Model: "gpt-test", KeyEnv: "TEST_LLM_KEY", BaseURL: baseURL})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return prov.(*openaiProvider)
}

func TestOpenAIComplete(t *testing.T) {
	var bodies []map[string]any
	var auth string
	srv := openaiServer(t, &bodies, &auth, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"olá"}}]}`)
	got, err := newOpenAI(t, srv.URL).Complete(context.Background(), "p")
	if err != nil || got != "olá" {
		t.Fatalf("Complete = %q, %v", got, err)
	}
	if auth != "Bearer sk-oa" || bodies[0]["model"] != "gpt-test" {
		t.Fatalf("auth = %q, body = %v", auth, bodies[0])
	}
	msgs := bodies[0]["messages"].([]any)
	if m := msgs[0].(map[string]any); len(msgs) != 1 || m["role"] != "user" || m["content"] != "p" {
		t.Fatalf("messages = %v", msgs)
	}
	if _, ok := bodies[0]["tools"]; ok {
		t.Fatalf("plain completion sent tools: %v", bodies[0])
	}
}

// TestOpenAICompleteToolsLoop (F-0323 crit. 10): the same neutral Tool reaches
// the model as function.parameters, the arguments string arrives as the raw
// input, and each call is answered with one role:tool message.
func TestOpenAICompleteToolsLoop(t *testing.T) {
	var bodies []map[string]any
	var auth string
	round1 := `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"refusal":null,"tool_calls":[` +
		`{"id":"call_1","type":"function","function":{"name":"cerne","arguments":"{\"verb\":\"reference new\",\"args\":[\"T\"]}"}},` +
		`{"id":"call_2","type":"function","function":{"name":"cerne","arguments":"{\"verb\":\"status\"}"}}]}}]}`
	round2 := `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"Registrei R-0001."}}]}`
	prov := newOpenAI(t, openaiServer(t, &bodies, &auth, round1, round2).URL)

	schema := map[string]any{"type": "object", "properties": map[string]any{"verb": map[string]any{"type": "string"}}}
	var calls []ToolCall
	got, err := prov.CompleteTools(context.Background(), "p", []Tool{{Name: "cerne", Description: "d", Schema: schema}},
		func(_ context.Context, c ToolCall) (string, bool) {
			calls = append(calls, c)
			return `{"ok":true}`, false
		})
	if err != nil || got != "Registrei R-0001." {
		t.Fatalf("CompleteTools = %q, %v", got, err)
	}
	if len(calls) != 1 || string(calls[0].Input) != `{"verb":"reference new","args":["T"]}` {
		t.Fatalf("calls = %+v, want only the round's first (C11)", calls)
	}
	if bodies[0]["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls = %v, want false", bodies[0]["parallel_tool_calls"])
	}
	fn := bodies[0]["tools"].([]any)[0].(map[string]any)
	if fn["type"] != "function" || !jsonEqual(fn["function"].(map[string]any)["parameters"], schema) || fn["function"].(map[string]any)["name"] != "cerne" {
		t.Fatalf("tool = %v, want the same schema as parameters", fn)
	}
	msgs := bodies[1]["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("2nd request messages = %v", msgs)
	}
	asst := msgs[1].(map[string]any)
	if asst["role"] != "assistant" || len(asst["tool_calls"].([]any)) != 2 {
		t.Fatalf("assistant echo = %v", asst)
	}
	if _, extra := asst["refusal"]; extra {
		t.Fatalf("assistant echo carries response-only fields: %v", asst)
	}
	for i, id := range []string{"call_1", "call_2"} {
		m := msgs[2+i].(map[string]any)
		want := []string{`{"ok":true}`, notExecuted}[i]
		if m["role"] != "tool" || m["tool_call_id"] != id || m["content"] != want {
			t.Fatalf("tool message %d = %v", i, m)
		}
	}
}

func TestOpenAICompleteToolsRoundLimit(t *testing.T) {
	var bodies []map[string]any
	var auth string
	loop := `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"c","type":"function","function":{"name":"cerne","arguments":"{}"}}]}}]}`
	prov := newOpenAI(t, openaiServer(t, &bodies, &auth, loop).URL)
	_, err := prov.CompleteTools(context.Background(), "p", nil, func(context.Context, ToolCall) (string, bool) { return "{}", false })
	if !errors.Is(err, ErrToolRounds) {
		t.Fatalf("err = %v, want ErrToolRounds", err)
	}
}

// A completion cut by the length limit never executes its tool calls.
func TestOpenAICompleteToolsRunsCallsOnlyWhenComplete(t *testing.T) {
	var bodies []map[string]any
	var auth string
	cut := `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":"parcial","tool_calls":[{"id":"c","type":"function","function":{"name":"cerne","arguments":"{"}}]}}]}`
	prov := newOpenAI(t, openaiServer(t, &bodies, &auth, cut).URL)
	got, err := prov.CompleteTools(context.Background(), "p", nil, func(context.Context, ToolCall) (string, bool) {
		t.Fatal("call ran on a length-cut completion")
		return "", false
	})
	if !errors.Is(err, ErrTruncated) || got != "parcial" {
		t.Fatalf("CompleteTools = %q, %v; want the partial text with ErrTruncated (C5)", got, err)
	}
}

func TestNewProviderOpenAIRequiresModel(t *testing.T) {
	t.Setenv("TEST_LLM_KEY", "sk")
	_, err := NewProvider(project.LLMConfig{Mode: "byo-key", Provider: "openai", KeyEnv: "TEST_LLM_KEY"})
	if !errors.Is(err, errModelRequired) {
		t.Fatalf("err = %v, want errModelRequired (no Anthropic default for openai)", err)
	}
	prov, err := NewProvider(project.LLMConfig{Mode: "byo-key", Provider: "openai", Model: "gpt-x", KeyEnv: "TEST_LLM_KEY"})
	if err != nil || prov.(*openaiProvider).baseURL != "https://api.openai.com" {
		t.Fatalf("NewProvider = %+v, %v, want the OpenAI default base URL", prov, err)
	}
}
