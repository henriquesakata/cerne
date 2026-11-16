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
