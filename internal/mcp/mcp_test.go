package mcp

import (
	"context"
	"testing"

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
	for _, want := range []string{"brief"} {
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
