package project

import "testing"

// F-0355 crit. 12: this repository's own .cernerc names its forge and PR
// template. Since F-0449 the same [delivery] block backs the delivery.* keys
// of the settings registry.
func TestRepoCernercDeliveryForge(t *testing.T) {
	cfg, _, err := LoadConfig("../..")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Delivery.Forge != "github" || cfg.Delivery.PRURL != "https://github.com/henriquesakata/cerne/pull/{n}" {
		t.Fatalf("delivery = %+v", cfg.Delivery)
	}
}
