package harness

import (
	"gitlab.com/rockt/cerne/internal/hooks"
)

// Gap is one artifact or capability a provider does not get (F-0259).
// Kind "degraded" = the provider's surface cannot host it (no fix);
// kind "missing" = installable, with the command that installs it.
type Gap struct {
	Name   string `json:"name"`
	Class  string `json:"class"`
	Kind   string `json:"kind"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

// ProviderCoverage summarizes one provider's slice of the inventory.
type ProviderCoverage struct {
	Provider string `json:"provider"`
	Detected bool   `json:"detected"`
	Total    int    `json:"total"`
	Gaps     []Gap  `json:"gaps"`
}

// coverage computes the per-provider summary over annotated classes.
// Hybrid by design (spec F-0259): hook capabilities come from the F-0174
// parity matrix (which already distinguishes degraded from installable);
// every other class contributes item-level gaps for provider-bound surfaces.
// The hooks class itself is excluded from item gaps — its cross-provider
// story IS the parity matrix, and counting both would double-report.
func coverage(projDir string, classes []Class) []ProviderCoverage {
	detected := map[string]bool{string(hooks.ProviderClaude): true}
	for _, p := range hooks.DetectProviders(projDir) {
		detected[string(p)] = true
	}

	var out []ProviderCoverage
	for _, p := range allProviders() {
		pc := ProviderCoverage{Provider: p, Detected: detected[p]}
		for _, c := range classes {
			for _, it := range c.Items {
				if consumes(it, p) {
					pc.Total++
				} else if c.Name != "providers" && c.Name != "hooks" {
					pc.Gaps = append(pc.Gaps, Gap{
						Name: it.Name, Class: c.Name, Kind: StateDegraded,
						Detail: "bound to a surface " + p + " does not read",
					})
				}
			}
		}
		for _, row := range hooks.ParityRows() {
			if deg, reason := row.Degraded(hooks.Provider(p)); deg {
				pc.Gaps = append(pc.Gaps, Gap{
					Name: row.Capability, Class: "providers", Kind: StateDegraded, Detail: reason,
				})
			} else if !detected[p] {
				pc.Gaps = append(pc.Gaps, Gap{
					Name: row.Capability, Class: "providers", Kind: StateMissing,
					Fix: "cerne hook install --provider " + p,
				})
			}
		}
		out = append(out, pc)
	}
	return out
}

func consumes(it Item, provider string) bool {
	for _, c := range it.Consumers {
		if c == provider {
			return true
		}
	}
	return false
}
