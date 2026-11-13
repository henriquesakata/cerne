package app

import "testing"

// F-0391 crit. 7: a vendor comes only from the models that spent the cost.
func TestCostVendors(t *testing.T) {
	for in, want := range map[string]string{"claude-opus-5-5": "anthropic", "gpt-5": "", "<synthetic>": ""} {
		if got := modelVendor(in); got != want {
			t.Errorf("modelVendor(%q) = %q, want %q", in, got, want)
		}
	}
}
