package server

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/project"
)

func TestValidLLMModeAcceptsSubscriptionCLIs(t *testing.T) {
	for _, m := range []string{"claude-cli", "codex-cli", "gemini-cli", "cursor-cli", "byo-key", "delegation"} {
		if !validLLMMode(m) {
			t.Errorf("%q rejected", m)
		}
	}
	if validLLMMode("nope") || validLLMMode("") {
		t.Error("invalid mode accepted")
	}
}

// F-0441 criterion 6: like codex and gemini, the Cursor CLI is given llm.model
// only when no byo-key provider is set, and the panel reports exactly that.
func TestLLMConfigModelForCursor(t *testing.T) {
	if got := llmConfig(project.LLMConfig{Mode: "cursor-cli", Model: "m"}, true).Model; got != "m" {
		t.Errorf("model = %q, want m", got)
	}
	if got := llmConfig(project.LLMConfig{Mode: "cursor-cli", Model: "m", Provider: "anthropic"}, true).Model; got != "" {
		t.Errorf("model with a byo-key provider = %q, want empty", got)
	}
}
