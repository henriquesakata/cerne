package llm

import (
	"context"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/project"
)

func TestResolveMode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ModeDelegation},
		{"delegation", ModeDelegation},
		{"byo-key", "byo-key"},
	}
	for _, c := range cases {
		if got := ResolveMode(c.in); got != c.want {
			t.Fatalf("ResolveMode(%q) = %q, want %q", c.in, got, c.want)
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
