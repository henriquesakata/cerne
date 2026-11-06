package llm

import (
	"context"
	"encoding/json"
	"errors"
)

// Tool is one function the model may call, in the neutral shape every provider
// adapter translates (F-0323): the catalog and its schemas belong to cerne,
// never to an adapter. Schema is a JSON Schema object.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
}

// ToolCall is one invocation the model emitted; Input is its raw JSON arguments.
type ToolCall struct {
	Name  string
	Input json.RawMessage
}

// ToolCaller is the optional capability of a function-calling provider: run a
// tool-use loop over tools, answering each call with call's content (isError
// marks a failed call), and return the model's final text. The loop lives in the
// adapter because each provider's message format differs; policy (confirmation,
// lifecycle, catalog) lives entirely in call.
type ToolCaller interface {
	CompleteTools(ctx context.Context, prompt string, tools []Tool, call func(context.Context, ToolCall) (content string, isError bool)) (string, error)
}

// ErrToolRounds is returned when the model keeps calling tools past maxToolRounds.
var ErrToolRounds = errors.New("tool-use loop exceeded its round limit")

// ErrTruncated is returned, with the partial text, when the model's output was
// cut by the token limit (F-0323 C5): nothing in the cut round was executed,
// and the caller must say so instead of presenting the turn as done.
var ErrTruncated = errors.New("model output truncated by the token limit: nothing in the cut round was executed")

const maxToolRounds = 16

// notExecuted answers every tool call of a round but the first (F-0323 C11):
// one call per round runs, so the model reads each real result — an id, an
// error — before it can aim the next write at it. Both adapters also ask the
// API for a single call per round; this covers a provider that ignores it.
const notExecuted = `{"ok":false,"output":"","error":{"code":"not_executed","message":"only the first tool call of a round runs: call it again after reading the previous result"}}`
