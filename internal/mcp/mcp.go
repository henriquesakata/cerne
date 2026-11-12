// Package mcp serves cerne's read-only commands as MCP tools (F-0363). Each
// tool runs the matching CLI command with --json through an injected Exec and
// returns its stdout untouched, so a tool's answer cannot drift from the
// command's: there is one JSON builder, the command itself.
package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Exec runs one cerne command line (e.g. ["brief", "--json"]) and returns its
// stdout. internal/cli provides it; the indirection keeps this package from
// importing internal/cli and lets tests inject a fake.
type Exec func(args []string) ([]byte, error)

type noArgs struct{}

// NewServer registers the tools on an MCP server named cerne.
func NewServer(version string, exec Exec) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "cerne", Version: version}, nil)
	add(s, exec, "brief", "The whole project roster as id, state and title per entity (features, decisions, notes, references…), no paths. The cheapest map of the project.",
		func(noArgs) ([]string, error) { return []string{"brief", "--json"}, nil })
	return s
}

// add registers one read-only tool whose handler turns the typed input into a
// command line and returns the command's stdout as text.
func add[In any](s *sdk.Server, exec Exec, name, desc string, argv func(In) ([]string, error)) {
	tool := &sdk.Tool{Name: name, Description: desc, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true}}
	sdk.AddTool(s, tool, func(_ context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
		args, err := argv(in)
		if err != nil {
			return nil, nil, err
		}
		out, err := exec(args)
		if err != nil {
			return nil, nil, err
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(out)}}}, nil, nil
	})
}
