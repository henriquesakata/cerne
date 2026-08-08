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

import "context"

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

// ResolveMode maps a configured .cernerc [llm] mode to the effective mode,
// defaulting an empty value to ModeDelegation.
func ResolveMode(mode string) string {
	if mode == "" {
		return ModeDelegation
	}
	return mode
}

// KnownModels lists the model ids offered by the Backlog chat's model
// selector (F-0218). Only the byo-key httpProvider currently honors an
// override (its Model field goes straight into the Messages API request);
// claude-cli deliberately never passes --model (see cliProvider.Complete).
var KnownModels = []string{
	"claude-opus-5",
	"claude-sonnet-5",
	"claude-fable-5",
	"claude-haiku-4-5-20251001",
}

// KnownEfforts lists the reasoning-effort levels offered alongside
// KnownModels. No provider applies Effort yet — accepted on the request for
// forward compatibility (F-0218 criterion 5: unsupported means silently
// ignored, not an error).
var KnownEfforts = []string{"low", "medium", "high", "xhigh", "max"}

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
