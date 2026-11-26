package project

import "testing"

func TestWithDefaultsProviderPresets(t *testing.T) {
	cases := []struct {
		provider, baseURL, keyEnv string
		model                     bool
	}{
		{"", "https://api.anthropic.com", "CERNE_LLM_API_KEY", true},
		{"anthropic", "https://api.anthropic.com", "CERNE_LLM_API_KEY", true},
		{"openai", "https://api.openai.com", "OPENAI_API_KEY", false},
		{"gemini", "https://generativelanguage.googleapis.com/v1beta/openai", "GEMINI_API_KEY", false},
		{"openrouter", "https://openrouter.ai/api", "OPENROUTER_API_KEY", false},
		{"compatible", "", "CERNE_LLM_API_KEY", false},
	}
	for _, c := range cases {
		got := LLMConfig{Provider: c.provider}.WithDefaults()
		if got.BaseURL != c.baseURL || got.KeyEnv != c.keyEnv || (got.Model != "") != c.model {
			t.Errorf("%q: base=%q keyEnv=%q model=%q, want base=%q keyEnv=%q model?=%v", c.provider, got.BaseURL, got.KeyEnv, got.Model, c.baseURL, c.keyEnv, c.model)
		}
	}
	// Explicit values always win over the preset.
	got := LLMConfig{Provider: "gemini", BaseURL: "http://x", KeyEnv: "K", Model: "m"}.WithDefaults()
	if got.BaseURL != "http://x" || got.KeyEnv != "K" || got.Model != "m" {
		t.Errorf("explicit values overridden: %+v", got)
	}
}
