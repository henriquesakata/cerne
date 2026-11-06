package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// providerOpenAI is the OpenAI-compatible byo-key provider (F-0323 crit. 10):
// OpenAI itself and the gateways that speak its Chat Completions API
// (OpenRouter, Ollama, vLLM).
const providerOpenAI = "openai"

// errModelRequired: the openai provider has no default model — the Anthropic
// default would never exist on an OpenAI-compatible endpoint.
var errModelRequired = errors.New("the openai provider requires an explicit [llm] model")

// openaiProvider speaks POST /v1/chat/completions over raw net/http. No
// Streamer (the chat falls back to its one-event stream, F-0240) and no model
// listing (ListModels answers ErrNoListing for this provider).
// ponytail: no max_tokens sent — OpenAI renamed it (max_completion_tokens) and
// gateways differ; the endpoint default applies. Map cfg.MaxTokens if a gateway truncates.
type openaiProvider struct {
	baseURL    string
	model      string
	apiKey     string
	httpClient *http.Client
}

type openaiRequest struct {
	Model    string       `json:"model"`
	Messages []any        `json:"messages"`
	Tools    []openaiTool `json:"tools,omitempty"`
	// ParallelToolCalls=false asks for one tool call per round (F-0323 C11).
	ParallelToolCalls *bool `json:"parallel_tool_calls,omitempty"`
}

type openaiTool struct {
	Type     string         `json:"type"`
	Function openaiFunction `json:"function"`
}

type openaiFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openaiMessage struct {
	Role       string          `json:"role"`
	Content    *string         `json:"content"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type openaiToolCall struct {
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// complete runs one chat completion and returns the first choice.
func (p *openaiProvider) complete(ctx context.Context, body openaiRequest) (finish string, msg openaiMessage, err error) {
	reqBody, err := json.Marshal(body)
	if err != nil {
		return "", msg, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", msg, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", msg, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", msg, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", msg, fmt.Errorf("OpenAI Chat Completions API returned status %d: %s", resp.StatusCode, truncate(respBody, maxErrBodyBytes))
	}
	var parsed struct {
		Choices []struct {
			FinishReason string        `json:"finish_reason"`
			Message      openaiMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil || len(parsed.Choices) == 0 {
		return "", msg, fmt.Errorf("unreadable API response: %s", truncate(respBody, maxErrBodyBytes))
	}
	return parsed.Choices[0].FinishReason, parsed.Choices[0].Message, nil
}

func text(m openaiMessage) string {
	if m.Content == nil {
		return ""
	}
	return *m.Content
}

// Complete performs one chat completion and returns its text.
func (p *openaiProvider) Complete(ctx context.Context, prompt string) (string, error) {
	_, msg, err := p.complete(ctx, openaiRequest{Model: p.model, Messages: []any{openaiMessage{Role: "user", Content: &prompt}}})
	return text(msg), err
}

// CompleteTools implements ToolCaller over Chat Completions: the same neutral
// tools as function.parameters; while the model returns tool_calls, the
// round's first runs through call and any other is answered as not executed
// (C11), one role:tool message each, the assistant turn echoed with
// only the request-side fields. A completion cut by the length limit executes
// nothing and returns ErrTruncated with its partial text (C5).
func (p *openaiProvider) CompleteTools(ctx context.Context, prompt string, tools []Tool, call func(context.Context, ToolCall) (string, bool)) (string, error) {
	defs := make([]openaiTool, len(tools))
	for i, t := range tools {
		defs[i] = openaiTool{Type: "function", Function: openaiFunction{Name: t.Name, Description: t.Description, Parameters: t.Schema}}
	}
	msgs := []any{openaiMessage{Role: "user", Content: &prompt}}
	for range maxToolRounds {
		req := openaiRequest{Model: p.model, Messages: msgs, Tools: defs}
		if len(defs) > 0 {
			req.ParallelToolCalls = new(bool)
		}
		finish, msg, err := p.complete(ctx, req)
		if err != nil {
			return "", err
		}
		var calls []openaiToolCall
		_ = json.Unmarshal(msg.ToolCalls, &calls)
		if finish == "length" {
			return text(msg), ErrTruncated
		}
		if len(calls) == 0 {
			return text(msg), nil
		}
		msgs = append(msgs, openaiMessage{Role: "assistant", Content: msg.Content, ToolCalls: msg.ToolCalls})
		for i, c := range calls {
			args := c.Function.Arguments
			if args == "" {
				args = "{}"
			}
			content := notExecuted
			if i == 0 { // C11: one call per round
				content, _ = call(ctx, ToolCall{Name: c.Function.Name, Input: json.RawMessage(args)})
			}
			msgs = append(msgs, openaiMessage{Role: "tool", Content: &content, ToolCallID: c.ID})
		}
	}
	return "", fmt.Errorf("%w (%d rounds)", ErrToolRounds, maxToolRounds)
}
