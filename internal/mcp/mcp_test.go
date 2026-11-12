package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeExec records every command line it is asked to run and answers with a
// fixed JSON body, so the tool surface is tested without a cerne project.
type fakeExec struct{ calls [][]string }

func (f *fakeExec) run(args []string) ([]byte, error) {
	f.calls = append(f.calls, args)
	return []byte("{\"ok\": true}\n"), nil
}

// connect starts the server on an in-memory transport and returns a client
// session already past the initialize handshake.
func connect(t *testing.T, exec Exec) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	if _, err := NewServer("test", exec).Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "v0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect (initialize): %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// TestMCP_ListsTools: criterion 1 — after the handshake, tools/list names the
// read-only tools, each marked read-only.
func TestMCP_ListsTools(t *testing.T) {
	cs := connect(t, (&fakeExec{}).run)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %s is not annotated read-only", tool.Name)
		}
	}
	for _, want := range []string{"status", "brief", "next", "search", "show", "ask"} {
		if !got[want] {
			t.Errorf("tools/list lacks %q; got %v", want, got)
		}
	}
}

// TestMCP_BriefRunsBriefJSON: the brief tool runs `brief --json` and returns
// the executor's bytes untouched as the text content.
func TestMCP_BriefRunsBriefJSON(t *testing.T) {
	fe := &fakeExec{}
	cs := connect(t, fe.run)
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "brief", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("call brief: err=%v res=%+v", err, res)
	}
	if len(fe.calls) != 1 || len(fe.calls[0]) != 2 || fe.calls[0][0] != "brief" || fe.calls[0][1] != "--json" {
		t.Fatalf("executor calls = %q, want [[brief --json]]", fe.calls)
	}
	if text := res.Content[0].(*sdk.TextContent).Text; text != "{\"ok\": true}\n" {
		t.Fatalf("content = %q, want the executor's bytes", text)
	}
}

// TestMCP_ToolArgv: every tool maps its typed fields, and only those, onto the
// matching command with --json.
func TestMCP_ToolArgv(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
		want []string
	}{
		{"status", map[string]any{}, []string{"status", "--json"}},
		{"status", map[string]any{"active": true}, []string{"status", "--json", "--active"}},
		{"next", map[string]any{}, []string{"next", "--json"}},
		{"search", map[string]any{"query": "login google"}, []string{"search", "login google", "--json"}},
		{"search", map[string]any{"query": "x", "limit": 5}, []string{"search", "x", "--json", "--limit", "5"}},
		{"show", map[string]any{"id": "F-0363"}, []string{"show", "F-0363", "--json"}},
		{"show", map[string]any{"id": "RB-0001"}, []string{"show", "RB-0001", "--json"}},
		{"ask", map[string]any{"question": "why sqlite?"}, []string{"ask", "why sqlite?", "--json"}},
	}
	for _, c := range cases {
		fe := &fakeExec{}
		cs := connect(t, fe.run)
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: c.tool, Arguments: c.args})
		if err != nil || res.IsError {
			t.Errorf("%s %v: err=%v res=%+v", c.tool, c.args, err, res)
			continue
		}
		if len(fe.calls) != 1 || !reflect.DeepEqual(fe.calls[0], c.want) {
			t.Errorf("%s %v: executor calls = %q, want [%q]", c.tool, c.args, fe.calls, c.want)
		}
	}
}

// TestMCP_RejectsFlagLikeArgs: a client value never becomes a flag or a path —
// a value starting with "-", an id off cerne's pattern, an out-of-range limit
// or an unknown field is refused before any command runs.
func TestMCP_RejectsFlagLikeArgs(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"show", map[string]any{"id": "--help"}},
		{"show", map[string]any{"id": "F-1"}},
		{"show", map[string]any{"id": "f-0001"}},
		{"show", map[string]any{"id": "../F-0001"}},
		{"show", map[string]any{"id": "F-0001 --all"}},
		{"show", map[string]any{}},
		{"show", map[string]any{"id": "F-0001", "dir": "/etc"}},
		{"search", map[string]any{"query": "-x"}},
		{"search", map[string]any{"query": "--dir=/etc"}},
		{"search", map[string]any{"query": ""}},
		{"search", map[string]any{"query": "x", "limit": 0}},
		{"search", map[string]any{"query": "x", "limit": 51}},
		{"ask", map[string]any{"question": "-h"}},
		{"ask", map[string]any{"question": "   "}},
		{"status", map[string]any{"all": true}},
	}
	for _, c := range cases {
		fe := &fakeExec{}
		cs := connect(t, fe.run)
		res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: c.tool, Arguments: c.args})
		if err == nil && !res.IsError {
			t.Errorf("%s %v: accepted, want refusal", c.tool, c.args)
		}
		if len(fe.calls) != 0 {
			t.Errorf("%s %v: executor ran %q, want no command", c.tool, c.args, fe.calls)
		}
	}
}

// TestMCP_InputSchemasCarryLimits: the published schemas state the boundary
// rules, so a client sees them before calling (the handler still checks).
func TestMCP_InputSchemasCarryLimits(t *testing.T) {
	cs := connect(t, (&fakeExec{}).run)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	prop := map[string]map[string]any{}
	for _, tool := range res.Tools {
		b, _ := json.Marshal(tool.InputSchema)
		var s struct {
			Properties map[string]map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(b, &s); err != nil {
			t.Fatal(err)
		}
		for name, p := range s.Properties {
			prop[tool.Name+"."+name] = p
		}
	}
	for key, want := range map[string]map[string]any{
		"search.limit": {"minimum": 1.0, "maximum": 50.0},
		"search.query": {"minLength": 1.0},
		"show.id":      {"pattern": `^[A-Z]+-\d{4,}$`},
		"ask.question": {"minLength": 1.0},
	} {
		for k, v := range want {
			if prop[key][k] != v {
				t.Errorf("%s.%s = %v, want %v (schema %v)", key, k, prop[key][k], v, prop[key])
			}
		}
	}
}

func callText(t *testing.T, cs *sdk.ClientSession, name string) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("call %s: protocol error %v, want a tool result", name, err)
	}
	return res.Content[0].(*sdk.TextContent).Text, res.IsError
}

// TestMCP_HungCallTimesOutThenBusy: a command that never returns costs its
// caller one deadline (a tool error), the next call is refused as busy at
// once instead of queueing behind it, and service resumes when it finishes.
func TestMCP_HungCallTimesOutThenBusy(t *testing.T) {
	old := callTimeout
	callTimeout = 100 * time.Millisecond
	t.Cleanup(func() { callTimeout = old })
	release := make(chan struct{})
	var n atomic.Int32
	cs := connect(t, func([]string) ([]byte, error) {
		if n.Add(1) == 1 {
			<-release
		}
		return []byte("{}\n"), nil
	})

	if text, isErr := callText(t, cs, "brief"); !isErr || !strings.Contains(text, "timed out") {
		t.Fatalf("hung call: isError=%v text=%q, want a timeout tool error", isErr, text)
	}
	start := time.Now()
	if text, isErr := callText(t, cs, "next"); !isErr || !strings.Contains(text, "busy") {
		t.Fatalf("call behind a hung one: isError=%v text=%q, want busy", isErr, text)
	}
	if d := time.Since(start); d > callTimeout/2 {
		t.Fatalf("busy refusal took %v: it blocked", d)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, isErr := callText(t, cs, "status"); !isErr {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("service did not resume after the hung command returned")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := n.Load(); got != 2 {
		t.Fatalf("exec ran %d times, want 2 (the busy call never runs)", got)
	}
}
