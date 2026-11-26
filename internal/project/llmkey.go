package project

import (
	"errors"
	"fmt"
	"strings"
)

// ErrKeyWrongVendor: the key found for a provider plainly belongs to another
// vendor (B-0092). It is never sent.
var ErrKeyWrongVendor = errors.New("API key belongs to another provider")

const anthropicKeyPrefix = "sk-ant-"

// keyPrefixVendor maps the key prefixes that name their vendor to it. A key of
// any other shape is never judged.
var keyPrefixVendor = []struct{ prefix, vendor string }{
	{anthropicKeyPrefix, llmProviderAnthropic},
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
// own variable, then — for anthropic only — the legacy CERNE_LLM_API_KEY
// (F-0523: a name two vendors shared handed one's key to the other). Each name
// is looked up in the process environment, then in the host credentials file.
// The legacy name yields only an Anthropic-shaped key, unless base_url is the
// user's own endpoint. A key that plainly belongs to another vendor is never
// returned (B-0092): ErrKeyWrongVendor names the variable.
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
	own := ProviderKeyEnv(c.Provider)
	if own != "" {
		v, src, err := ResolveAPIKey(own)
		if vendor := c.foreignKey(v); vendor != "" {
			return "", "", own, wrongVendor(own, vendor, c.Provider)
		}
		if err != nil || v != "" {
			return v, src, own, err
		}
	}
	if own == providerKeyEnv[llmProviderAnthropic] {
		v, src, err := ResolveAPIKey(defaultLLMKeyEnv)
		if err != nil {
			return "", "", defaultLLMKeyEnv, err
		}
		if v != "" && (c.BaseURL != "" || strings.HasPrefix(v, anthropicKeyPrefix)) {
			return v, src, defaultLLMKeyEnv, nil
		}
	}
	return "", "", c.KeyVar(), nil
}

// LegacyKeyNote says where the key goes now when openai finds none and the
// legacy variable still holds one (F-0523); "" otherwise. It names the
// variables, never the key.
func (c LLMConfig) LegacyKeyNote() string {
	if c.Provider != llmProviderOpenAI || c.KeyEnv != "" {
		return ""
	}
	own := providerKeyEnv[llmProviderOpenAI]
	if v, _, _ := ResolveAPIKey(own); v != "" {
		return ""
	}
	// A key there that names another vendor was never OpenAI's: nothing to move.
	if v, _, _ := ResolveAPIKey(defaultLLMKeyEnv); v == "" || c.foreignKey(v) != "" {
		return ""
	}
	return fmt.Sprintf("%s is no longer read for OpenAI — put the OpenAI key in %s", defaultLLMKeyEnv, own)
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
