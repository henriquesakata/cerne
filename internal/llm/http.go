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
	"os"
	"slices"
	"strings"

	"gitlab.com/rockt/cerne/internal/models"
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
	}
	return []string{cfg.WithDefaults().Provider}
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
// claude-cli differs from NewProvider: the CLI runs as an agent scoped by an
// allowlist, working directory = dir (the project root, so the cerne commands
// hit the right backend). Every other mode delegates to NewProvider unchanged:
// byo-key stays the static one-shot HTTP call (no tool loop in v1, criterion 3)
// and delegation keeps its sentinel.
//
// write (F-0260) is the caller's report that the USER confirmed this turn, and
// it only widens the allowlist for that one call — the capability lives on the
// request, never on the conversation. False is the default everywhere, so a
// caller that forgets it gets the read-only agent.
func NewAgentProvider(cfg project.LLMConfig, dir string, write bool) (Provider, error) {
	cfg = cfg.WithDefaults()
	if ResolveMode(cfg.Mode) == ModeClaudeCLI {
		tools := AgentReadTools
		denied := AgentDeniedPaths
		if write {
			tools = append(append([]string{}, AgentReadTools...), AgentWriteTools...)
			// F-0269: the broad grant only makes sense paired with the refusals —
			// an unconfirmed turn has no write to refuse.
			denied = append(append([]string{}, AgentDeniedPaths...), AgentDeniedCommands...)
		}
		return &cliProvider{bin: cfg.ClaudeBin, run: agentRun, dir: dir, agentTools: tools, deniedTools: denied}, nil
	}
	return NewProvider(cfg)
}

// messagesRequest is the request body for POST /v1/messages. Only the fields the
// single-shot completion needs are sent: no temperature/top_p/top_k/thinking
// (they 400 on the default model).
type messagesRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Stream    bool             `json:"stream,omitempty"`
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
	cfg = cfg.WithDefaults()
	if mode := ResolveMode(cfg.Mode); mode == ModeDelegation || mode == ModeClaudeCLI {
		return nil, fmt.Errorf("%w (mode %s)", ErrNoListing, mode)
	}
	if cfg.Provider != providerAnthropic {
		return nil, fmt.Errorf("%w (provider %q)", ErrNoListing, cfg.Provider)
	}
	key := os.Getenv(cfg.KeyEnv)
	if key == "" {
		return nil, fmt.Errorf("%w: set the %s environment variable", ErrAPIKeyMissing, cfg.KeyEnv)
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

// Complete performs one Messages API call and returns the concatenated text of
// every "text"-type content block.
func (p *httpProvider) Complete(ctx context.Context, prompt string) (string, error) {
	resp, err := p.post(ctx, messagesRequest{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		Messages:  []messagesMember{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
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
	resp, err := p.post(ctx, messagesRequest{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		Stream:    true,
		Messages:  []messagesMember{{Role: "user", Content: prompt}},
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
