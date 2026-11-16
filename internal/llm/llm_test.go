package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

func TestResolveMode(t *testing.T) {
	cases := []struct{ in, detected, want, src string }{
		{"", "", ModeDelegation, SourceDefault},
		{"", "claude", ModeClaudeCLI, SourceDetected},
		{"", "codex", ModeCodexCLI, SourceDetected},
		{"", "gemini", ModeGeminiCLI, SourceDetected},
		{"", "cursor-agent", ModeCursorCLI, SourceDetected},
		{"", "copilot", ModeCopilotCLI, SourceDetected},
		{"delegation", "claude", ModeDelegation, SourceConfigured},
		{"byo-key", "codex", "byo-key", SourceConfigured},
		{"gemini-cli", "", ModeGeminiCLI, SourceConfigured},
	}
	orig := detectCLI
	t.Cleanup(func() { detectCLI = orig })
	for _, c := range cases {
		detectCLI = func() string { return c.detected }
		got, src := ResolveModeSource(c.in)
		if got != c.want || src != c.src || ResolveMode(c.in) != c.want {
			t.Fatalf("ResolveModeSource(%q) with %q = %q/%q, want %q/%q", c.in, c.detected, got, src, c.want, c.src)
		}
	}
}

func TestFakeComplete(t *testing.T) {
	f := &Fake{Reply: "resposta"}
	got, err := f.Complete(context.Background(), "prompt-x")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got != "resposta" {
		t.Fatalf("Complete = %q, want %q", got, "resposta")
	}
	if len(f.Prompts) != 1 || f.Prompts[0] != "prompt-x" {
		t.Fatalf("recorded Prompts = %v, want [prompt-x]", f.Prompts)
	}
}

// TestConfiguredProviders: F-0311 criterion 14 — the vendors a project's
// [llm] config already reaches; delegation reaches none.
func TestConfiguredProviders(t *testing.T) {
	cases := []struct {
		cfg  project.LLMConfig
		want string
	}{
		{project.LLMConfig{}, ""},
		{project.LLMConfig{Mode: ModeClaudeCLI}, "anthropic"},
		{project.LLMConfig{Mode: ModeByoKey}, "anthropic"},
		{project.LLMConfig{Mode: ModeByoKey, Provider: "vendor"}, "vendor"},
	}
	for _, c := range cases {
		if got := strings.Join(ConfiguredProviders(c.cfg), ","); got != c.want {
			t.Fatalf("ConfiguredProviders(%+v) = %q, want %q", c.cfg, got, c.want)
		}
	}
}

func TestMain_DetectionOffByDefault(t *testing.T) {
	if got, src := ResolveModeSource(""); got != ModeDelegation || src != SourceDefault {
		t.Fatalf("under go test an empty mode must stay delegation, got %q/%q", got, src)
	}
}

// F-0414 (M2): ResolveMode asks the project detector at call time, so one
// replaced seam (project.DetectSubscriptionCLI) steers both layers.
func TestResolveModeFollowsTheProjectDetector(t *testing.T) {
	orig := project.DetectSubscriptionCLI
	t.Cleanup(func() { project.DetectSubscriptionCLI = orig })
	project.DetectSubscriptionCLI = func() string { return "gemini" }
	if got := ResolveMode(""); got != ModeGeminiCLI {
		t.Fatalf("ResolveMode = %q, want gemini-cli", got)
	}
}

// TestModeForProviderInvertsConfiguredProviders (F-0440): every subscription
// CLI mode maps to its vendor and back; any other vendor has no CLI mode.
func TestModeForProviderInvertsConfiguredProviders(t *testing.T) {
	for _, mode := range []string{ModeClaudeCLI, ModeCodexCLI, ModeGeminiCLI, ModeCursorCLI, ModeCopilotCLI} {
		p := ConfiguredProviders(project.LLMConfig{Mode: mode})
		if got := ModeForProvider(p[0]); got != mode {
			t.Fatalf("ModeForProvider(%q) = %q, want %q", p[0], got, mode)
		}
	}
	for _, p := range []string{"", "openrouter", "compatible", "vendor"} {
		if got := ModeForProvider(p); got != "" {
			t.Fatalf("ModeForProvider(%q) = %q, want empty", p, got)
		}
	}
}

// TestForProviderSwitchesOnlyTheCopy (F-0440): a vendor switch swaps the mode
// and drops what belonged to the project's vendor (provider, model, effort); the
// configured vendor and an unknown one leave the config as it was.
func TestForProviderSwitchesOnlyTheCopy(t *testing.T) {
	cfg := project.LLMConfig{Mode: ModeClaudeCLI, Provider: "vendor", Model: "claude-x", Effort: "max"}
	got := ForProvider(cfg, "openai")
	if got.Mode != ModeCodexCLI || got.Provider != "" || got.Model != "" || got.Effort != "" {
		t.Fatalf("switch = %+v", got)
	}
	if cfg.Mode != ModeClaudeCLI || cfg.Model != "claude-x" {
		t.Fatalf("the original was mutated: %+v", cfg)
	}
	for _, p := range []string{"", "anthropic", "nope"} {
		if got := ForProvider(cfg, p); got != cfg {
			t.Fatalf("ForProvider(%q) = %+v, want unchanged", p, got)
		}
	}
}
