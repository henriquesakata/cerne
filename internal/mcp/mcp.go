// Package mcp serves cerne's read-only commands as MCP tools (F-0363). Each
// tool runs the matching CLI command with --json through an injected Exec and
// returns its stdout untouched, so a tool's answer cannot drift from the
// command's: there is one JSON builder, the command itself.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Exec runs one cerne command line (e.g. ["brief", "--json"]) and returns its
// stdout. internal/cli provides it; the indirection keeps this package from
// importing internal/cli and lets tests inject a fake.
type Exec func(args []string) ([]byte, error)

// ErrBadArgument is a tool argument refused at the boundary, before any
// command runs.
var ErrBadArgument = errors.New("invalid argument")

// idPattern is cerne's entity id shape (prefix + at least four digits), the
// same pattern internal/store checks; anything else never reaches `show`.
const idPattern = `^[A-Z]+-\d{4,}$`

var idRe = regexp.MustCompile(idPattern)

type noArgs struct{}

type statusArgs struct {
	Active bool `json:"active,omitempty" jsonschema:"only active work: hides implemented, superseded and rejected features"`
}

type searchArgs struct {
	Query string `json:"query" jsonschema:"full-text query over every document, history included"`
	Limit *int   `json:"limit,omitempty" jsonschema:"maximum number of results, 1 to 50 (default 10)"`
}

type showArgs struct {
	ID string `json:"id" jsonschema:"entity id, e.g. F-0363, D-0001, N-0028, R-0061, RB-0001"`
}

type askArgs struct {
	Question string `json:"question" jsonschema:"natural-language question; returns the retrieved sources, no LLM call"`
}

// NewServer registers the tools on an MCP server named cerne.
func NewServer(version string, exec Exec) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "cerne", Version: version}, nil)
	add(s, exec, "status", "Feature states and the epics rollup: what exists and in which lifecycle state.", nil,
		func(in statusArgs) ([]string, error) {
			if in.Active {
				return []string{"status", "--json", "--active"}, nil
			}
			return []string{"status", "--json"}, nil
		})
	add(s, exec, "brief", "The whole project roster as id, state and title per entity (features, decisions, notes, references…), no paths. The cheapest map of the project.", nil,
		func(noArgs) ([]string, error) { return []string{"brief", "--json"}, nil })
	add(s, exec, "next", "What to do next: active features grouped by their next action, plus the documentation debts lint finds.", nil,
		func(noArgs) ([]string, error) { return []string{"next", "--json"}, nil })
	add(s, exec, "search", "Find entities and documents by subject (full-text, history included).",
		func(p map[string]*jsonschema.Schema) {
			p["query"].MinLength = jsonschema.Ptr(1)
			p["limit"].Type, p["limit"].Types = "integer", nil
			p["limit"].Minimum, p["limit"].Maximum = jsonschema.Ptr(1.0), jsonschema.Ptr(50.0)
		},
		func(in searchArgs) ([]string, error) {
			if err := plainText("query", in.Query); err != nil {
				return nil, err
			}
			args := []string{"search", in.Query, "--json"}
			if in.Limit != nil {
				if *in.Limit < 1 || *in.Limit > 50 {
					return nil, fmt.Errorf("limit %d is outside 1..50: %w", *in.Limit, ErrBadArgument)
				}
				args = append(args, "--limit", strconv.Itoa(*in.Limit))
			}
			return args, nil
		})
	add(s, exec, "show", "Read one entity by id: its metadata and its documents.",
		func(p map[string]*jsonschema.Schema) { p["id"].Pattern = idPattern },
		func(in showArgs) ([]string, error) {
			if !idRe.MatchString(in.ID) {
				return nil, fmt.Errorf("id %q is not a cerne id (e.g. F-0363): %w", in.ID, ErrBadArgument)
			}
			return []string{"show", in.ID, "--json"}, nil
		})
	add(s, exec, "ask", "Retrieve the sources that answer a question, ranked, with snippets. Makes no LLM call: you answer from the sources.",
		func(p map[string]*jsonschema.Schema) { p["question"].MinLength = jsonschema.Ptr(1) },
		func(in askArgs) ([]string, error) {
			if err := plainText("question", in.Question); err != nil {
				return nil, err
			}
			return []string{"ask", in.Question, "--json"}, nil
		})
	return s
}

// plainText refuses a blank value or one that cobra would parse as a flag.
func plainText(field, v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("%s is empty: %w", field, ErrBadArgument)
	}
	if strings.HasPrefix(v, "-") {
		return fmt.Errorf("%s %q starts with \"-\": %w", field, v, ErrBadArgument)
	}
	return nil
}

// add registers one read-only tool whose handler turns the typed input into a
// command line and returns the command's stdout as text.
// shape, when set, adds the boundary rules to the schema inferred from In, so
// the client sees them; argv still enforces them.
func add[In any](s *sdk.Server, exec Exec, name, desc string, shape func(props map[string]*jsonschema.Schema), argv func(In) ([]string, error)) {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("tool %s: %v", name, err)) // a programming error in the In type
	}
	if shape != nil {
		shape(schema.Properties)
	}
	tool := &sdk.Tool{Name: name, Description: desc, InputSchema: schema, Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true}}
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
