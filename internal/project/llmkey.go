package project

import (
	"errors"
	"fmt"
	"strings"
)

// ErrKeyWrongVendor: the key found for a provider plainly belongs to another
// vendor (B-0092). It is never sent.
var ErrKeyWrongVendor = errors.New("API key belongs to another provider")

// keyPrefixVendor maps the key prefixes that name their vendor to it. A key of
// any other shape is never judged.
var keyPrefixVendor = []struct{ prefix, vendor string }{
	{"sk-ant-", llmProviderAnthropic},
	{"sk-or-", llmProviderOpenRouter},
	{"sk-proj-", llmProviderOpenAI},
	{"AIza", llmProviderGemini},
}

// foreignKey reports the vendor a key belongs to when that is plainly not the
// provider about to receive it (B-0092: an Anthropic key in the shared legacy
// variable went to OpenAI's endpoint). A custom base_url and the compatible
// provider are the user's own endpoint — a proxy may take any key — so nothing
// is judged there.
func (c LLMConfig) foreignKey(key string) string {
	provider := c.Provider
	if provider == "" {
		provider = defaultLLMProvider
	}
	if c.BaseURL != "" || provider == llmProviderCompatible {
		return ""
	}
	for _, k := range keyPrefixVendor {
		if strings.HasPrefix(key, k.prefix) && k.vendor != provider {
			return k.vendor
		}
	}
	return ""
}

// providerKeyEnv is the variable (and credentials-file name) each byo-key
// provider's key lives under (F-0414): one name per vendor, so saving or
// exporting one provider's key can never reach another provider's endpoint.
var providerKeyEnv = map[string]string{
	llmProviderAnthropic:  "ANTHROPIC_API_KEY",
	llmProviderOpenAI:     "OPENAI_API_KEY",
	llmProviderGemini:     "GEMINI_API_KEY",
	llmProviderOpenRouter: "OPENROUTER_API_KEY",
	llmProviderCompatible: "CERNE_COMPATIBLE_API_KEY",
}

// ProviderKeyEnv is the variable provider's key lives under; "" for a provider
// cerne does not know. The empty provider is the default, anthropic.
func ProviderKeyEnv(provider string) string {
	if provider == "" {
		provider = defaultLLMProvider
	}
	return providerKeyEnv[provider]
}

// ResolveKey finds the API key of the configured provider (c is the raw
// config, before WithDefaults): an explicit key_env alone, else the provider's
// own variable, then — for anthropic and openai only, F-0323's single shared
// name — the legacy CERNE_LLM_API_KEY. Each name is looked up in the process
// environment, then in the host credentials file. The compatible provider
// never reads the legacy name: its endpoint is not the vendor that key is for.
// A key that plainly belongs to another vendor is never returned (B-0092): the
// legacy name is skipped, any other variable is ErrKeyWrongVendor.
// env is the variable the key was found under, the provider's own when none was.
func (c LLMConfig) ResolveKey() (value, source, env string, err error) {
	// An explicit key_env is the user pinning one variable: nothing else is read.
	if c.KeyEnv != "" {
		v, src, err := ResolveAPIKey(c.KeyEnv)
		if vendor := c.foreignKey(v); vendor != "" {
			return "", "", c.KeyEnv, wrongVendor(c.KeyEnv, vendor, c.Provider)
		}
		return v, src, c.KeyEnv, err
	}
	names := []string{}
	own := ProviderKeyEnv(c.Provider)
	if own != "" {
		names = append(names, own)
	}
	if p := c.Provider; p == "" || p == llmProviderAnthropic || p == llmProviderOpenAI {
		names = append(names, defaultLLMKeyEnv)
	}
	for _, n := range names {
		v, src, err := ResolveAPIKey(n)
		if vendor := c.foreignKey(v); vendor != "" {
			// The shared legacy name holds one vendor's key: for the other
			// provider it is simply not this provider's key.
			if n == defaultLLMKeyEnv && n != own {
				continue
			}
			return "", "", n, wrongVendor(n, vendor, c.Provider)
		}
		if err != nil || v != "" {
			return v, src, n, err
		}
	}
	return "", "", c.KeyVar(), nil
}

// wrongVendor names the variable, the key's vendor and the provider — never
// the key.
func wrongVendor(env, vendor, provider string) error {
	if provider == "" {
		provider = defaultLLMProvider
	}
	return fmt.Errorf("%w: %s holds a %s key and the provider is %s — the key was not sent; put the %s key in %s", ErrKeyWrongVendor, env, vendor, provider, provider, ProviderKeyEnv(provider))
}

// KeyVar is the variable the key is read from first: the explicit key_env, else
// the provider's own.
func (c LLMConfig) KeyVar() string {
	if c.KeyEnv != "" {
		return c.KeyEnv
	}
	return ProviderKeyEnv(c.Provider)
}
