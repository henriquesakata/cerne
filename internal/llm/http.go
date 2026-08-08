package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"gitlab.com/rockt/cerne/internal/project"
)

// ErrNoProviderForDelegation is returned by NewProvider when the config resolves
// to delegation mode: there is no Provider to build there (the host agent does
// the synthesis). Callers should only call NewProvider for non-delegation modes;
// this sentinel lets them detect a misuse.
var ErrNoProviderForDelegation = errors.New("delegation mode has no Provider")

// ErrAPIKeyMissing is returned by NewProvider when the env var named by
// LLMConfig.KeyEnv is unset or empty.
var ErrAPIKeyMissing = errors.New("LLM API key not set in environment")

// providerAnthropic is the only concrete byo-key provider implemented.
const providerAnthropic = "anthropic"

// anthropicVersion is the required value of the anthropic-version header.
const anthropicVersion = "2023-06-01"

// httpProvider is a minimal Provider over the Anthropic Messages API using raw
// net/http — no SDK. It performs a single non-streaming completion per call.
type httpProvider struct {
	baseURL    string
	model      string
	maxTokens  int
	apiKey     string
	httpClient *http.Client
}

// NewProvider builds the configured Provider. It applies LLMConfig defaults
// first, then routes on the resolved mode:
//   - delegation: ErrNoProviderForDelegation (no provider exists there);
//   - claude-cli: an *cliProvider shelling to cfg.ClaudeBin via the real
//     exec runner — NO API key is read (auth is the Claude Code subscription);
//   - byo-key (default for any other mode): only provider "anthropic" (any
//     other value errors, naming it), reads the API key from os.Getenv(cfg.KeyEnv)
//     (ErrAPIKeyMissing when empty), and returns an *httpProvider.
func NewProvider(cfg project.LLMConfig) (Provider, error) {
	cfg = cfg.WithDefaults()
	switch ResolveMode(cfg.Mode) {
	case ModeDelegation:
		return nil, ErrNoProviderForDelegation
	case ModeClaudeCLI:
		return &cliProvider{bin: cfg.ClaudeBin, run: execRun}, nil
	}
	if cfg.Provider != providerAnthropic {
		return nil, fmt.Errorf("LLM provider %q not supported (only %q)", cfg.Provider, providerAnthropic)
	}
	key := os.Getenv(cfg.KeyEnv)
	if key == "" {
		return nil, fmt.Errorf("%w: set the %s environment variable", ErrAPIKeyMissing, cfg.KeyEnv)
	}
	return &httpProvider{
		baseURL:    cfg.BaseURL,
		model:      cfg.Model,
		maxTokens:  cfg.MaxTokens,
		apiKey:     key,
		httpClient: http.DefaultClient,
	}, nil
}

// NewAgentProvider builds the provider for the Backlog chat (F-0224). Only
// claude-cli differs from NewProvider: the CLI runs as a read-only agent —
// AgentReadTools allowlist, working directory = dir (the project root, so the
// cerne commands hit the right backend). Every other mode delegates to
// NewProvider unchanged: byo-key stays the static one-shot HTTP call (no tool
// loop in v1, criterion 3) and delegation keeps its sentinel.
func NewAgentProvider(cfg project.LLMConfig, dir string) (Provider, error) {
	cfg = cfg.WithDefaults()
	if ResolveMode(cfg.Mode) == ModeClaudeCLI {
		return &cliProvider{bin: cfg.ClaudeBin, run: execRun, dir: dir, agentTools: AgentReadTools}, nil
	}
	return NewProvider(cfg)
}

// messagesRequest is the request body for POST /v1/messages. Only the fields the
// single-shot completion needs are sent: no temperature/top_p/top_k/thinking
// (they 400 on the default model).
type messagesRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Messages  []messagesMember `json:"messages"`
}

type messagesMember struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// messagesResponse is the subset of the response we read: the content blocks.
type messagesResponse struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// maxErrBodyBytes bounds how much of a non-2xx response body is echoed back in
// the error, so a large HTML error page does not flood the message.
const maxErrBodyBytes = 512

// Complete performs one Messages API call and returns the concatenated text of
// every "text"-type content block. Non-2xx responses become an error including
// the status code and a truncated slice of the body.
func (p *httpProvider) Complete(ctx context.Context, prompt string) (string, error) {
	reqBody, err := json.Marshal(messagesRequest{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		Messages:  []messagesMember{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Anthropic Messages API returned status %d: %s", resp.StatusCode, truncate(respBody, maxErrBodyBytes))
	}

	var parsed messagesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("unreadable API response: %w", err)
	}
	var b bytes.Buffer
	for _, block := range parsed.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String(), nil
}

// truncate returns at most n bytes of data as a string, appending an ellipsis
// marker when it had to cut.
func truncate(data []byte, n int) string {
	if len(data) <= n {
		return string(data)
	}
	return string(data[:n]) + "…"
}
