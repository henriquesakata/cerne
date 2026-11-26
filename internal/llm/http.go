package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/models"
	"github.com/henriquesakata/cerne/internal/project"
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

// The other byo-key providers (F-0414) all speak the OpenAI adapter; only the
// preset (base URL, key variable) differs.
const (
	providerGemini     = "gemini"
	providerOpenRouter = "openrouter"
	providerCompatible = "compatible"
)

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

// ConfiguredProviders names the model vendors the project's [llm] config
// already reaches (F-0311): none in delegation, the Claude Code subscription's
// vendor for claude-cli, the configured byo-key provider otherwise. Read-only:
// registering a provider or a key is not this seam's job.
func ConfiguredProviders(cfg project.LLMConfig) []string {
	switch ResolveMode(cfg.Mode) {
	case ModeDelegation:
		return nil
	case ModeClaudeCLI:
		return []string{providerAnthropic}
	case ModeCodexCLI:
		return []string{providerOpenAI}
	case ModeGeminiCLI:
		return []string{"gemini"}
	case ModeCursorCLI:
		return []string{"cursor"}
	case ModeCopilotCLI:
		return []string{"copilot"}
	}
	return []string{cfg.WithDefaults().Provider}
}

// ModeForProvider is the inverse of ConfiguredProviders for the subscription
// CLIs: the mode whose vendor is provider, "" when none (F-0440).
func ModeForProvider(provider string) string {
	for _, mode := range []string{ModeClaudeCLI, ModeCodexCLI, ModeGeminiCLI, ModeCursorCLI, ModeCopilotCLI} {
		if p := ConfiguredProviders(project.LLMConfig{Mode: mode}); len(p) == 1 && p[0] == provider {
			return mode
		}
	}
	return ""
}

// NewProvider builds the configured Provider. It applies LLMConfig defaults
// first, then routes on the resolved mode:
//   - delegation: ErrNoProviderForDelegation (no provider exists there);
//   - claude-cli: an *cliProvider shelling to cfg.ClaudeBin via the real
//     exec runner — NO API key is read (auth is the Claude Code subscription);
//   - byo-key (default for any other mode): provider "anthropic" or "openai"
//     (F-0323; any other value errors, naming it), reads the API key from os.Getenv(cfg.KeyEnv)
//     (ErrAPIKeyMissing when empty), and returns an *httpProvider.
//
// cliModel is the model a subscription CLI is asked for: llm.model only when no
// provider is set. A model chosen for a byo-key provider (an OpenAI or an
// Anthropic id) must never reach a CLI that would reject it or silently differ (F-0414).
func cliModel(c project.LLMConfig) string {
	if c.Provider != "" {
		return ""
	}
	return c.Model
}

func NewProvider(cfg project.LLMConfig) (Provider, error) {
	raw := cfg // WithDefaults fills an Anthropic model and a key variable the CLIs and the key lookup must not see
	cfg = cfg.WithDefaults()
	switch mode := ResolveMode(cfg.Mode); mode {
	case ModeDelegation:
		return nil, ErrNoProviderForDelegation
	case ModeClaudeCLI:
		return &cliProvider{bin: cfg.ClaudeBin, run: execRun}, nil
	case ModeCodexCLI:
		return &cliProvider{kind: mode, bin: "codex", model: cliModel(raw), effort: raw.Effort, run: execRun}, nil
	case ModeGeminiCLI:
		return &cliProvider{kind: mode, bin: "gemini", model: cliModel(raw), run: execRun}, nil
	case ModeCursorCLI:
		return &cliProvider{kind: mode, bin: "cursor-agent", model: cliModel(raw), run: execRun}, nil
	case ModeCopilotCLI:
		return &cliProvider{kind: mode, bin: "copilot", model: cliModel(raw), effort: raw.Effort, run: execRun}, nil
	}
	switch cfg.Provider {
	case providerAnthropic, providerOpenAI, providerGemini, providerOpenRouter, providerCompatible:
	default:
		return nil, fmt.Errorf("LLM provider %q not supported (use anthropic, openai, gemini, openrouter or compatible)", cfg.Provider)
	}
	if cfg.Model == "" {
		return nil, errModelRequired
	}
	if cfg.BaseURL == "" {
		return nil, errBaseURLRequired
	}
	key, _, _, err := raw.ResolveKey()
	if err != nil {
		return nil, err
	}
	if cfg.TestKey != "" {
		key = cfg.TestKey
	}
	if key == "" && cfg.Provider != providerCompatible {
		err := fmt.Errorf("%w: set the %s environment variable or save the key in Settings", ErrAPIKeyMissing, raw.KeyVar())
		if note := raw.LegacyKeyNote(); note != "" {
			err = fmt.Errorf("%w (%s)", err, note)
		}
		return nil, err
	}
	if cfg.Provider != providerAnthropic {
		return &openaiProvider{baseURL: cfg.BaseURL, model: cfg.Model, apiKey: key, httpClient: http.DefaultClient}, nil
	}
	return &httpProvider{
		baseURL:    cfg.BaseURL,
		model:      cfg.Model,
		maxTokens:  cfg.MaxTokens,
		apiKey:     key,
		httpClient: http.DefaultClient,
	}, nil
}

// NewAgentProvider builds the provider for the Backlog chat (F-0224).
//
// write (F-0260) is the caller's report that the USER confirmed this turn, and
// it only widens the allowlist for that one call — the capability lives on the
// request, never on the conversation. False is the default everywhere, so a
// caller that forgets it gets the read-only agent.
func NewAgentProvider(cfg project.LLMConfig, dir string, write bool) (Provider, error) {
	raw := cfg
	cfg = cfg.WithDefaults()
	if ResolveMode(cfg.Mode) == ModeCodexCLI && dir != "" {
		return &cliProvider{kind: ModeCodexCLI, bin: "codex", model: cliModel(raw), effort: raw.Effort, run: execRun, dir: dir}, nil
	}
	if ResolveMode(cfg.Mode) == ModeClaudeCLI {
		tools := AgentReadTools
		denied := AgentDeniedPaths
		// F-0323: no derived refusals, no broad grant (fail closed).
		if write && len(AgentDeniedCommands) > 0 {
			tools = append(append([]string{}, AgentReadTools...), AgentWriteTools...)
			// F-0269: the broad grant only makes sense paired with the refusals —
			// an unconfirmed turn has no write to refuse.
			denied = append(append([]string{}, AgentDeniedPaths...), AgentDeniedCommands...)
		}
		p := &cliProvider{bin: cfg.ClaudeBin, run: agentRun, dir: dir, agentTools: tools, deniedTools: denied}
		// B-0007: the composer's model and effort reach the turn, but only the
		// Anthropic ones — another vendor's id would be rejected by claude.
		if raw.Provider == "" || raw.Provider == providerAnthropic {
			if claudeModelRE.MatchString(raw.Model) {
				p.model = raw.Model
			}
			if slices.Contains(claudeEfforts, raw.Effort) {
				p.effort = raw.Effort
			}
		}
		return p, nil
	}
	return NewProvider(raw)
}

// messagesRequest is the request body for POST /v1/messages. Only the fields the
// single-shot completion needs are sent: no temperature/top_p/top_k/thinking
// (they 400 on the default model).
type messagesRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Stream    bool             `json:"stream,omitempty"`
	Messages  []messagesMember `json:"messages"`
	Tools     []anthropicTool  `json:"tools,omitempty"`
	// ToolChoice asks for one tool call per round (F-0323 C11).
	ToolChoice *anthropicToolChoice `json:"tool_choice,omitempty"`
}

type anthropicToolChoice struct {
	Type                   string `json:"type"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use"`
}

// messagesMember.Content is a string for plain turns and a block list for the
// tool loop's turns; a string serializes exactly as before.
type messagesMember struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
}

// messagesResponse is the subset of the response we read: the content blocks.
type messagesResponse struct {
	Model   string         `json:"model"`
	Content []contentBlock `json:"content"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		CacheRead    int `json:"cache_read_input_tokens"`
		CacheWrite   int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// maxErrBodyBytes bounds how much of a non-2xx response body is echoed back in
// the error, so a large HTML error page does not flood the message.
const maxErrBodyBytes = 512

// post sends one Messages API request and returns the response with a 2xx
// status; any other status is turned into an error carrying the code and a
// truncated slice of the body. The caller owns closing the body on success.
func (p *httpProvider) post(ctx context.Context, body messagesRequest) (*http.Response, error) {
	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return p.send(req, "Messages")
}

// send sets the Anthropic API auth headers on req and returns a 2xx response;
// any other status becomes an error naming api, the code and a truncated body.
// The caller owns closing the body on success.
func (p *httpProvider) send(req *http.Request, api string) (*http.Response, error) {
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("Anthropic %s API returned status %d: %s", api, resp.StatusCode, truncate(respBody, maxErrBodyBytes))
	}
	return resp, nil
}

// ErrNoListing: the configured mode has no way to list the provider's models
// (delegation reaches no provider; claude-cli rides the Claude Code
// subscription, which exposes no listing). The catalog keeps its baseline.
var ErrNoListing = errors.New("this LLM mode cannot list the provider's models")

// ErrEmptyListing: the provider answered with no model at all — treated as a
// failure, never as "every model vanished".
var ErrEmptyListing = errors.New("the provider listed no models")

// PickProvider chooses whom the catalog refresh queries (F-0311 criterion
// 17): the persisted preference while the project still reaches it, else the
// only configured provider, else nobody.
func PickProvider(preferred string, configured []string) string {
	if slices.Contains(configured, preferred) {
		return preferred
	}
	if len(configured) == 1 {
		return configured[0]
	}
	return ""
}

// modelsListResponse is the subset of GET /v1/models we read.
type modelsListResponse struct {
	Data []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"data"`
}

// ListModels asks the provider which models it serves (F-0311): GET
// /v1/models with the Messages API auth headers. Only byo-key "anthropic" can;
// any other mode is ErrNoListing and a missing key ErrAPIKeyMissing, both
// without touching the network. One page of up to 1000 models.
func ListModels(ctx context.Context, cfg project.LLMConfig) ([]models.Entry, error) {
	raw := cfg
	cfg = cfg.WithDefaults()
	if mode := ResolveMode(cfg.Mode); mode == ModeDelegation || mode == ModeClaudeCLI || mode == ModeCodexCLI || mode == ModeGeminiCLI || mode == ModeCursorCLI || mode == ModeCopilotCLI {
		return nil, fmt.Errorf("%w (mode %s)", ErrNoListing, mode)
	}
	if cfg.Provider != providerAnthropic {
		return nil, fmt.Errorf("%w (provider %q)", ErrNoListing, cfg.Provider)
	}
	key, _, _, err := raw.ResolveKey()
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, fmt.Errorf("%w: set the %s environment variable or save the key in Settings", ErrAPIKeyMissing, raw.KeyVar())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/v1/models?limit=1000", nil)
	if err != nil {
		return nil, err
	}
	p := &httpProvider{apiKey: key, httpClient: http.DefaultClient}
	resp, err := p.send(req, "Models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var parsed modelsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("unreadable models listing: %w", err)
	}
	out := make([]models.Entry, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if m.ID != "" {
			out = append(out, models.Entry{ID: m.ID, Label: m.DisplayName})
		}
	}
	if len(out) == 0 {
		return nil, ErrEmptyListing
	}
	return out, nil
}

// anthropicUserContent is the content of the turn's user message: the prompt
// as a plain string, or — when the turn has attached images (F-0522) — one
// base64 image block per file followed by the prompt as a text block.
func anthropicUserContent(ctx context.Context, prompt string) (any, error) {
	imgs, err := loadImages(ctx)
	if err != nil || len(imgs) == 0 {
		return prompt, err
	}
	blocks := make([]any, 0, len(imgs)+1)
	for _, img := range imgs {
		blocks = append(blocks, map[string]any{"type": "image", "source": map[string]string{"type": "base64", "media_type": img.mediaType, "data": img.base64}})
	}
	return append(blocks, map[string]string{"type": "text", "text": prompt}), nil
}

// Complete performs one Messages API call and returns the concatenated text of
// every "text"-type content block.
func (p *httpProvider) Complete(ctx context.Context, prompt string) (string, error) {
	s, _, err := p.CompleteUsage(ctx, prompt)
	return s, err
}

// CompleteUsage is Complete that also returns the tokens the response reports
// (F-0502); the usage travels with the call, never in the shared provider.
func (p *httpProvider) CompleteUsage(ctx context.Context, prompt string) (string, Usage, error) {
	content, err := anthropicUserContent(ctx, prompt)
	if err != nil {
		return "", Usage{}, err
	}
	resp, err := p.post(ctx, messagesRequest{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		Messages:  []messagesMember{{Role: "user", Content: content}},
	})
	if err != nil {
		return "", Usage{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", Usage{}, err
	}
	var parsed messagesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", Usage{}, fmt.Errorf("unreadable API response: %w", err)
	}
	u := parsed.Usage
	usage := Usage{Model: parsed.Model, InputTokens: u.InputTokens + u.CacheRead + u.CacheWrite, OutputTokens: u.OutputTokens}
	var b bytes.Buffer
	for _, block := range parsed.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String(), usage, nil
}

// CompleteTools implements ToolCaller over the Messages API (F-0323): while
// the model stops on tool_use, the round's first tool_use runs through call and
// any other is answered as not executed (C11), one tool_result each in a single
// user turn; the assistant turn is
// echoed verbatim (thinking blocks included, as the API requires). Calls run
// only on stop_reason tool_use — a turn cut by max_tokens executes nothing and
// returns ErrTruncated with its partial text (C5). Returns the final turn's text.
func (p *httpProvider) CompleteTools(ctx context.Context, prompt string, tools []Tool, call func(context.Context, ToolCall) (string, bool)) (string, error) {
	defs := make([]anthropicTool, len(tools))
	for i, t := range tools {
		defs[i] = anthropicTool{Name: t.Name, Description: t.Description, InputSchema: t.Schema}
	}
	content, err := anthropicUserContent(ctx, prompt)
	if err != nil {
		return "", err
	}
	msgs := []messagesMember{{Role: "user", Content: content}}
	for range maxToolRounds {
		req := messagesRequest{Model: p.model, MaxTokens: p.maxTokens, Messages: msgs, Tools: defs}
		if len(defs) > 0 {
			req.ToolChoice = &anthropicToolChoice{Type: "auto", DisableParallelToolUse: true}
		}
		resp, err := p.post(ctx, req)
		if err != nil {
			return "", err
		}
		var parsed struct {
			StopReason string            `json:"stop_reason"`
			Content    []json.RawMessage `json:"content"`
		}
		err = json.NewDecoder(resp.Body).Decode(&parsed)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("unreadable API response: %w", err)
		}
		var text strings.Builder
		var uses []contentBlock
		for _, raw := range parsed.Content {
			var b contentBlock
			if json.Unmarshal(raw, &b) != nil {
				continue
			}
			switch b.Type {
			case "text":
				text.WriteString(b.Text)
			case "tool_use":
				uses = append(uses, b)
			}
		}
		if parsed.StopReason == "max_tokens" {
			return text.String(), ErrTruncated
		}
		if parsed.StopReason != "tool_use" || len(uses) == 0 {
			return text.String(), nil
		}
		results := make([]toolResultBlock, len(uses))
		for i, u := range uses {
			content, isErr := notExecuted, true
			if i == 0 { // C11: one call per round
				content, isErr = call(ctx, ToolCall{Name: u.Name, Input: u.Input})
			}
			results[i] = toolResultBlock{Type: "tool_result", ToolUseID: u.ID, Content: content, IsError: isErr}
		}
		msgs = append(msgs, messagesMember{Role: "assistant", Content: parsed.Content}, messagesMember{Role: "user", Content: results})
	}
	return "", fmt.Errorf("%w (%d rounds)", ErrToolRounds, maxToolRounds)
}

// sseEvent is the subset of a Messages API streaming event we read: text
// deltas and the in-stream error event.
type sseEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Stream implements Streamer (F-0240): same call as Complete but with
// stream:true, reading the SSE response and forwarding each text_delta to
// onDelta. The return is the full concatenated text. An onDelta error aborts
// the read and is returned as-is.
func (p *httpProvider) Stream(ctx context.Context, prompt string, onDelta func(string) error) (string, error) {
	content, err := anthropicUserContent(ctx, prompt)
	if err != nil {
		return "", err
	}
	resp, err := p.post(ctx, messagesRequest{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		Stream:    true,
		Messages:  []messagesMember{{Role: "user", Content: content}},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var b bytes.Buffer
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		var ev sseEvent
		if json.Unmarshal([]byte(data), &ev) != nil {
			continue
		}
		switch {
		case ev.Type == "error":
			return b.String(), fmt.Errorf("Anthropic Messages API stream error: %s", ev.Error.Message)
		case ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" && ev.Delta.Text != "":
			b.WriteString(ev.Delta.Text)
			if err := onDelta(ev.Delta.Text); err != nil {
				return b.String(), err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return b.String(), err
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

// ForProvider is cfg as a turn on provider's subscription CLI sees it (F-0440).
// The configured vendor, an empty or an unknown one leave it as it is; a switch
// drops what belonged to the project's vendor (provider, model, effort).
func ForProvider(cfg project.LLMConfig, provider string) project.LLMConfig {
	mode := ModeForProvider(provider)
	if provider == "" || mode == "" || slices.Contains(ConfiguredProviders(cfg), provider) {
		return cfg
	}
	cfg.Mode, cfg.Provider, cfg.Model, cfg.Effort = mode, "", "", ""
	return cfg
}
