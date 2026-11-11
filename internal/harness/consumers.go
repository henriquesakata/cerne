package harness

import (
	"strings"

	"github.com/henriquesakata/cerne/internal/hooks"
)

// allProviders is the known provider set, in the fixed display order.
func allProviders() []string {
	return []string{
		string(hooks.ProviderClaude),
		string(hooks.ProviderCursor),
		string(hooks.ProviderCodex),
		string(hooks.ProviderGemini),
	}
}

// consumersFor reports which AI providers can consume one inventory item
// (F-0259). Deterministic on class/origin/name — no inspection, no LLM.
func consumersFor(it Item) []string {
	switch it.Class {
	case "cerne":
		// .cernerc, docs.lock, .docs/ — any agent reads the projection.
		return allProviders()
	case "providers":
		// Parity rows are named "<provider>: <capability>".
		if i := strings.Index(it.Name, ":"); i > 0 {
			return []string{it.Name[:i]}
		}
		return nil
	case "context":
		// AGENTS.md: Codex reads it natively (R-0035), Cursor supports it at
		// the root (R-0037); Gemini only via context.fileName, which the
		// inventory does not inspect.
		if strings.Contains(it.Name, "AGENTS.md") {
			return []string{string(hooks.ProviderCodex), string(hooks.ProviderCursor)}
		}
		return []string{string(hooks.ProviderClaude)}
	default:
		// hooks, skills, config, plugins, mcp: .claude/** and marketplace
		// surfaces — except a PATH binary, which any agent's hooks can call.
		if it.Origin == OriginPath {
			return allProviders()
		}
		return []string{string(hooks.ProviderClaude)}
	}
}

// annotateConsumers stamps the consumers list on every item of every class.
func annotateConsumers(classes []Class) {
	for ci := range classes {
		for ii := range classes[ci].Items {
			classes[ci].Items[ii].Consumers = consumersFor(classes[ci].Items[ii])
		}
	}
}
