package project

import (
	"errors"
	"strings"
	"testing"
)

func clearLLMKeys(t *testing.T) {
	t.Helper()
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	for _, n := range []string{"CERNE_LLM_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(n, "")
	}
}

// F-0523: CERNE_LLM_API_KEY is Anthropic's alone.
func TestLegacyKeyIsAnthropicOnly(t *testing.T) {
	const ant, old = "sk-ant-api03-SECRETSECRET", "sk-oldstyleSECRET"
	cases := []struct {
		name            string
		cfg             LLMConfig
		env             map[string]string
		wantKey, wantIn string
	}{
		// F-0523 crit. 1
		{"openai never reads legacy", LLMConfig{Provider: "openai"}, map[string]string{"CERNE_LLM_API_KEY": old}, "", "OPENAI_API_KEY"},
		{"openai reads its own", LLMConfig{Provider: "openai"}, map[string]string{"OPENAI_API_KEY": old, "CERNE_LLM_API_KEY": ant}, old, "OPENAI_API_KEY"},
		// F-0523 crit. 2
		{"anthropic ignores a non-Anthropic legacy value", LLMConfig{Provider: "anthropic"}, map[string]string{"CERNE_LLM_API_KEY": old}, "", "ANTHROPIC_API_KEY"},
		{"default ignores a non-Anthropic legacy value", LLMConfig{}, map[string]string{"CERNE_LLM_API_KEY": old}, "", "ANTHROPIC_API_KEY"},
		{"anthropic own variable first", LLMConfig{Provider: "anthropic"}, map[string]string{"ANTHROPIC_API_KEY": "sk-ant-own", "CERNE_LLM_API_KEY": ant}, "sk-ant-own", "ANTHROPIC_API_KEY"},
		// F-0523 crit. 3
		{"anthropic with base_url reads any legacy value", LLMConfig{Provider: "anthropic", BaseURL: "https://proxy.internal"}, map[string]string{"CERNE_LLM_API_KEY": old}, old, "CERNE_LLM_API_KEY"},
		{"anthropic with base_url reads even another vendor's legacy value", LLMConfig{Provider: "anthropic", BaseURL: "https://proxy.internal"}, map[string]string{"CERNE_LLM_API_KEY": "sk-proj-x"}, "sk-proj-x", "CERNE_LLM_API_KEY"},
		// F-0523 crit. 5
		{"pinned legacy for openai", LLMConfig{Provider: "openai", KeyEnv: "CERNE_LLM_API_KEY"}, map[string]string{"CERNE_LLM_API_KEY": old}, old, "CERNE_LLM_API_KEY"},
		// F-0523 crit. 6
		{"anthropic legacy unchanged", LLMConfig{Provider: "anthropic"}, map[string]string{"CERNE_LLM_API_KEY": ant}, ant, "CERNE_LLM_API_KEY"},
		{"default legacy unchanged", LLMConfig{}, map[string]string{"CERNE_LLM_API_KEY": ant}, ant, "CERNE_LLM_API_KEY"},
	}
	for _, c := range cases {
		clearLLMKeys(t)
		for k, v := range c.env {
			t.Setenv(k, v)
		}
		v, _, env, err := c.cfg.ResolveKey()
		if v != c.wantKey || env != c.wantIn || err != nil {
			t.Errorf("%s: key match=%v env=%q err=%v, want env %q", c.name, v == c.wantKey, env, err, c.wantIn)
		}
	}

	// F-0523 crit. 1: a key Settings once saved under the legacy name, in the
	// credentials file, is not openai's either.
	clearLLMKeys(t)
	if err := WriteCredential("CERNE_LLM_API_KEY", old); err != nil {
		t.Fatal(err)
	}
	if v, _, env, err := (LLMConfig{Provider: "openai"}).ResolveKey(); v != "" || env != "OPENAI_API_KEY" || err != nil {
		t.Errorf("openai with the legacy name in the credentials file: key set=%v env=%q err=%v", v != "", env, err)
	}
	// F-0523 crit. 4
	if n := (LLMConfig{Provider: "openai"}).LegacyKeyNote(); n == "" {
		t.Error("legacy name in the credentials file: want the note")
	}

	// F-0523 crit. 5: the pin keeps the B-0092 guard.
	clearLLMKeys(t)
	t.Setenv("CERNE_LLM_API_KEY", ant)
	if _, _, _, err := (LLMConfig{Provider: "openai", KeyEnv: "CERNE_LLM_API_KEY"}).ResolveKey(); !errors.Is(err, ErrKeyWrongVendor) {
		t.Errorf("pinned legacy with an Anthropic key for openai: err = %v, want ErrKeyWrongVendor", err)
	}
}

// F-0523 crit. 4
func TestLegacyKeyNote(t *testing.T) {
	const secret = "sk-oldstyleSECRET"
	clearLLMKeys(t)
	t.Setenv("CERNE_LLM_API_KEY", secret)
	note := LLMConfig{Provider: "openai"}.LegacyKeyNote()
	if !strings.Contains(note, "CERNE_LLM_API_KEY") || !strings.Contains(note, "OPENAI_API_KEY") || strings.Contains(note, "SECRET") {
		t.Fatalf("note = %q, want both variable names and no key", note)
	}
	for name, cfg := range map[string]LLMConfig{
		"anthropic": {Provider: "anthropic"},
		"default":   {},
		"gemini":    {Provider: "gemini"},
		"pinned":    {Provider: "openai", KeyEnv: "CERNE_LLM_API_KEY"},
	} {
		if n := cfg.LegacyKeyNote(); n != "" {
			t.Errorf("%s: note = %q, want none", name, n)
		}
	}
	t.Setenv("OPENAI_API_KEY", "sk-proj-own")
	if n := (LLMConfig{Provider: "openai"}).LegacyKeyNote(); n != "" {
		t.Errorf("openai with its own key: note = %q, want none", n)
	}
	for _, other := range []string{"sk-ant-api03-SECRETSECRET", "sk-or-v1-x", "AIzaSyX"} {
		clearLLMKeys(t)
		t.Setenv("CERNE_LLM_API_KEY", other)
		if n := (LLMConfig{Provider: "openai"}).LegacyKeyNote(); n != "" {
			t.Errorf("another vendor's key (%.6s…) in the legacy variable: note = %q, want none", other, n)
		}
	}
	clearLLMKeys(t)
	if n := (LLMConfig{Provider: "openai"}).LegacyKeyNote(); n != "" {
		t.Errorf("legacy variable empty: note = %q, want none", n)
	}
}
