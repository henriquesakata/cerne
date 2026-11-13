package app

import "testing"

// F-0391 crit. 7: a vendor comes only from what the data names.
func TestCostVendors(t *testing.T) {
	for in, want := range map[string]string{"claude-opus-5-5": "anthropic", "gpt-5": "", "<synthetic>": ""} {
		if got := modelVendor(in); got != want {
			t.Errorf("modelVendor(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"claude-cli": "anthropic", "anthropic": "anthropic", " OpenAI ": "openai", "": ""} {
		if got := providerVendor(in); got != want {
			t.Errorf("providerVendor(%q) = %q, want %q", in, got, want)
		}
	}
}
