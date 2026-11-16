// Package llm hosts the pluggable LLM provider seam for the optional v1.5
// layer (compile/ask). The deterministic core never imports it: only the
// compile and ask use-cases do, and only outside delegation mode.
//
// In delegation mode (the default and only runtime mode implemented now) the
// host agent already running — Claude Code, Cursor — performs the synthesis,
// so Provider.Complete is NOT called: the work travels through prompt files
// (compile) or stdout (ask). Complete exists for the test Fake and for a
// future BYO-key implementation behind the same interface.
package llm

import (
	"context"

	"github.com/henriquesakata/cerne/internal/project"
)

// ModeDelegation is the default provider mode: cerne emits prompts for the
// host agent instead of calling an LLM itself. No API key, no HTTP.
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

// ModeCodexCLI, ModeGeminiCLI and ModeCursorCLI are the subscription modes that
// shell to the Codex CLI, the Gemini CLI (F-0414) and the Cursor CLI (F-0441),
// like ModeClaudeCLI does for Claude Code.
const (
	ModeCodexCLI  = "codex-cli"
	ModeGeminiCLI = "gemini-cli"
	ModeCursorCLI = "cursor-cli"
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
