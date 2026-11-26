package project

import (
	"errors"
	"strings"
	"testing"
)

// B-0092: a key that plainly belongs to another vendor never reaches a
// provider's endpoint. The shared legacy variable is skipped when its key is
// not this provider's; a variable that names the provider, or one the user
// pinned, is an error that names the variable and never the key.
func TestResolveKeyRefusesAnotherVendorsKey(t *testing.T) {
	const antKey = "sk-ant-api03-SECRETSECRET"
	clear := func() {
		t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
		for _, n := range []string{"CERNE_LLM_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY", "CERNE_COMPATIBLE_API_KEY", "MY_KEY"} {
			t.Setenv(n, "")
		}
	}

	clear()
	t.Setenv("CERNE_LLM_API_KEY", antKey)
	if v, _, env, err := (LLMConfig{Provider: "openai"}).ResolveKey(); v != "" || err != nil || env != "OPENAI_API_KEY" {
		t.Fatalf("openai with an Anthropic key in the legacy variable: key set=%v env=%q err=%v, want no key, OPENAI_API_KEY, no error", v != "", env, err)
	}
	if v, _, env, err := (LLMConfig{Provider: "anthropic"}).ResolveKey(); v != antKey || err != nil || env != "CERNE_LLM_API_KEY" {
		t.Fatalf("anthropic must still read its key from the legacy variable: env=%q err=%v", env, err)
	}
	if v, _, _, err := (LLMConfig{}).ResolveKey(); v != antKey || err != nil {
		t.Fatalf("the default provider must still read the legacy variable: err=%v", err)
	}

	for name, tc := range map[string]struct {
		cfg LLMConfig
		env string
	}{
		"own variable":   {LLMConfig{Provider: "openai"}, "OPENAI_API_KEY"},
		"pinned key_env": {LLMConfig{Provider: "openai", KeyEnv: "MY_KEY"}, "MY_KEY"},
		"default openai": {LLMConfig{Provider: "openai", KeyEnv: "CERNE_LLM_API_KEY"}, "CERNE_LLM_API_KEY"},
	} {
		clear()
		t.Setenv(tc.env, antKey)
		v, _, _, err := tc.cfg.ResolveKey()
		if v != "" || !errors.Is(err, ErrKeyWrongVendor) {
			t.Errorf("%s: key set=%v err=%v, want no key and ErrKeyWrongVendor", name, v != "", err)
			continue
		}
		if msg := err.Error(); !strings.Contains(msg, tc.env) || strings.Contains(msg, "SECRET") || strings.Contains(msg, "sk-ant") {
			t.Errorf("%s: error must name the variable and never the key: %q", name, msg)
		}
	}

	// The provider's own variable still wins over a foreign key in the legacy one.
	clear()
	t.Setenv("CERNE_LLM_API_KEY", antKey)
	t.Setenv("OPENAI_API_KEY", "sk-proj-mine")
	if v, _, env, err := (LLMConfig{Provider: "openai"}).ResolveKey(); v != "sk-proj-mine" || env != "OPENAI_API_KEY" || err != nil {
		t.Errorf("own variable beside a foreign legacy key: env=%q err=%v, want the key of OPENAI_API_KEY", env, err)
	}

	// Other directions of the same mistake.
	for provider, key := range map[string]string{"anthropic": "sk-proj-abc", "openrouter": "sk-ant-abc", "gemini": "sk-or-v1-abc"} {
		clear()
		t.Setenv(ProviderKeyEnv(provider), key)
		if v, _, _, err := (LLMConfig{Provider: provider}).ResolveKey(); v != "" || !errors.Is(err, ErrKeyWrongVendor) {
			t.Errorf("%s with %s…: key set=%v err=%v, want ErrKeyWrongVendor", provider, key[:6], v != "", err)
		}
	}

	// A custom base_url or the compatible provider is the user's own endpoint
	// (a proxy may take any key), and a key of no known shape is never judged.
	clear()
	t.Setenv("OPENAI_API_KEY", antKey)
	if v, _, _, err := (LLMConfig{Provider: "openai", BaseURL: "https://proxy.internal"}).ResolveKey(); v != antKey || err != nil {
		t.Errorf("custom base_url: err=%v, want the key as configured", err)
	}
	t.Setenv("CERNE_COMPATIBLE_API_KEY", antKey)
	if v, _, _, err := (LLMConfig{Provider: "compatible"}).ResolveKey(); v != antKey || err != nil {
		t.Errorf("compatible: err=%v, want the key as configured", err)
	}
	t.Setenv("OPENAI_API_KEY", "sk-oldstyle123")
	if v, _, _, err := (LLMConfig{Provider: "openai"}).ResolveKey(); v != "sk-oldstyle123" || err != nil {
		t.Errorf("unknown key shape: err=%v, want the key as configured", err)
	}
}
