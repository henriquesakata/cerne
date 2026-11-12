package project

import "testing"

// F-0355 crit. 12: this repository's own .cernerc names its forge and PR
// template. (`cerne settings get delivery.forge` has no registry key: adding one
// needs the pt-BR catalog in ui/src/i18n.tsx, outside this feature.)
func TestRepoCernercDeliveryForge(t *testing.T) {
	cfg, _, err := LoadConfig("../..")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Delivery.Forge != "github" || cfg.Delivery.PRURL != "https://github.com/henriquesakata/cerne/pull/{n}" {
		t.Fatalf("delivery = %+v", cfg.Delivery)
	}
}
