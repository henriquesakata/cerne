// Package llm hosts the pluggable LLM provider seam for the optional LLM
// layer (ask, compile, the document drafts, the Backlog chat). The
// deterministic core (domain, lint, materialize) never imports it.
//
// ResolveMode picks the mode. A configured mode wins; an empty one resolves to
// the first subscription CLI found on PATH (claude-cli, codex-cli, gemini-cli,
// cursor-cli) and, with none, to delegation. The CLI modes shell to the agent's
// CLI under the user's subscription; byo-key calls a provider's HTTP API with a
// key from the environment. In delegation mode the host agent already running
// performs the synthesis, so Provider.Complete is NOT called: the work travels
// through prompt files (compile) or stdout (ask).
package llm

import (
	"context"

	"github.com/henriquesakata/cerne/internal/project"
)

// ModeDelegation is the fallback provider mode, used when no mode is configured
// and no subscription CLI is on PATH: cerne emits prompts for the host agent
// instead of calling an LLM itself. No API key, no HTTP.
const ModeDelegation = "delegation"

// ModeByoKey is the bring-your-own-key mode: cerne calls the provider's HTTP
// API with a key read from the environment (NewProvider's fallback mode).
const ModeByoKey = "byo-key"

// Provider abstracts a single text completion.
type Provider interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// Streamer is the optional capability of completing in parts (F-0240): onDelta
// receives each text chunk as it arrives; the return is still the full text.
// A provider that does not implement it keeps serving through Complete —
// callers discover the capability by type assertion.
type Streamer interface {
	Stream(ctx context.Context, prompt string, onDelta func(string) error) (string, error)
}

// Usage is what one completion consumed, as the provider reported it (F-0502).
// InputTokens counts every input-side token the provider billed, cache reads
// and writes included.
type Usage struct {
	Model                     string
	InputTokens, OutputTokens int
}

// UsageReporter is the optional capability of reporting the consumption of the
// last Complete; callers discover it by type assertion. A provider that does
// not report it (delegation, the other CLIs) simply lacks the method.
type UsageReporter interface {
	LastUsage() Usage
}

// ModeCodexCLI, ModeGeminiCLI and ModeCursorCLI are the subscription modes that
// shell to the Codex CLI, the Gemini CLI (F-0414) and the Cursor CLI (F-0441),
// like ModeClaudeCLI does for Claude Code.
const (
	ModeCodexCLI   = "codex-cli"
	ModeGeminiCLI  = "gemini-cli"
	ModeCursorCLI  = "cursor-cli"
	ModeCopilotCLI = "copilot-cli" // the GitHub Copilot CLI (F-0445)
)

// detectCLI is the PATH probe behind the dynamic default; tests replace it.
var detectCLI = func() string { return project.DetectSubscriptionCLI() }

// Sources of the effective mode (ResolveModeSource).
const (
	SourceConfigured = "configured"
	SourceDetected   = "detected"
	SourceDefault    = "default"
)

// ResolveMode maps a configured [llm] mode to the effective mode. An empty
// value resolves to the subscription CLI found on PATH (claude-cli, codex-cli,
// gemini-cli, cursor-cli) and to ModeDelegation when there is none.
func ResolveMode(mode string) string {
	m, _ := ResolveModeSource(mode)
	return m
}

// ResolveModeSource is ResolveMode plus where the answer came from: a stored
// value is "configured", a PATH hit "detected", the fallback "default".
func ResolveModeSource(mode string) (string, string) {
	if mode != "" {
		return mode, SourceConfigured
	}
	if cli := detectCLI(); cli != "" {
		return project.CLIMode(cli), SourceDetected
	}
	return ModeDelegation, SourceDefault
}

// Fake is a deterministic Provider for tests: it returns Reply and records
// every prompt it was given, so tests can assert on prompt assembly without
// calling a real LLM.
type Fake struct {
	Reply   string
	Prompts []string
}

// Complete records the prompt and returns the canned Reply.
func (f *Fake) Complete(_ context.Context, prompt string) (string, error) {
	f.Prompts = append(f.Prompts, prompt)
	return f.Reply, nil
}

// AnsweredModel is the model a provider's CLI reported it ran on its last call,
// "" when the provider reports none.
func AnsweredModel(p Provider) string {
	if r, ok := p.(interface{ AnsweredModel() string }); ok {
		return r.AnsweredModel()
	}
	return ""
}
