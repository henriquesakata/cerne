package harness

import (
	"path/filepath"
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
		string(hooks.ProviderCopilot),
	}
}

// OriginCursorProject / OriginCursorUser mark hook/config items sourced from
// .cursor/hooks.json (project) or ~/.cursor/hooks.json (user).
const (
	OriginCursorProject = "cursor-project"
	OriginCursorUser    = "cursor-user"
)

// consumersFor reports which AI providers can consume one inventory item
// (F-0259, F-0526). Deterministic on class/origin/name/path — no I/O.
func consumersFor(it Item, copilot bool) []string {
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
		// inventory does not inspect; Copilot reads it in all three surfaces.
		if strings.Contains(it.Name, "AGENTS.md") {
			return []string{string(hooks.ProviderCodex), string(hooks.ProviderCursor), string(hooks.ProviderCopilot)}
		}
		// Project-root CLAUDE.md: Cursor loads it natively (R-0058, R-0154).
		if it.Name == "CLAUDE.md" && it.Origin == OriginProject {
			return []string{string(hooks.ProviderClaude), string(hooks.ProviderCursor)}
		}
		// ~/.claude/CLAUDE.md (OriginUser) stays claude-only.
		return []string{string(hooks.ProviderClaude)}
	case "mcp":
		// .cursor/mcp.json (project or user) is Cursor's alone (F-0512).
		if filepath.Base(filepath.Dir(it.Path)) == ".cursor" {
			return []string{string(hooks.ProviderCursor)}
		}
		// The project .mcp.json is also Copilot's workspace MCP file (F-0444) —
		// counted only when the project uses Copilot.
		if it.Origin == OriginProject && copilot {
			return []string{string(hooks.ProviderClaude), string(hooks.ProviderCopilot)}
		}
		return []string{string(hooks.ProviderClaude)}
	case "config":
		// .cernerc is cerne's own config — read by any provider.
		if it.Name == ".cernerc" {
			return allProviders()
		}
		// .cursor/hooks.json entries — Cursor's config surface only.
		if it.Origin == OriginCursorProject || it.Origin == OriginCursorUser {
			return []string{string(hooks.ProviderCursor)}
		}
		if filepath.Base(filepath.Dir(it.Path)) == ".cursor" {
			return []string{string(hooks.ProviderCursor)}
		}
		// Claude Code settings layers: Cursor imports .claude/* by default
		// (R-0037, "Include Third-Party Plugins, Skills, and Other Configs").
		return []string{string(hooks.ProviderClaude), string(hooks.ProviderCursor)}
	case "plugins":
		// enabledPlugins: Cursor imports the same plugin set via its
		// third-party import toggle (R-0037, R-0066, R-0154).
		return []string{string(hooks.ProviderClaude), string(hooks.ProviderCursor)}
	case "skills":
		p := filepath.ToSlash(it.Path)
		// .cursor/skills/** or ~/.cursor/skills/** → Cursor only.
		if strings.HasPrefix(p, ".cursor/") || strings.Contains(p, "/.cursor/") {
			return []string{string(hooks.ProviderCursor)}
		}
		// .agents/skills/** or ~/.agents/skills/** → agent-skills readers.
		if strings.HasPrefix(p, ".agents/") || strings.Contains(p, "/.agents/") {
			return []string{string(hooks.ProviderCursor), string(hooks.ProviderCodex), string(hooks.ProviderGemini), string(hooks.ProviderCopilot)}
		}
		// .claude/skills/** (project, local, user): Cursor imports them.
		return []string{string(hooks.ProviderClaude), string(hooks.ProviderCursor)}
	case "hooks":
		// A PATH binary runs in any agent's shell.
		if it.Origin == OriginPath {
			return allProviders()
		}
		// Cursor-sourced hook entries.
		if it.Origin == OriginCursorProject || it.Origin == OriginCursorUser {
			return []string{string(hooks.ProviderCursor)}
		}
		// Git hooks run on every git commit regardless of the agent.
		if it.Name == "prepare-commit-msg" || it.Name == "commit-msg" {
			return allProviders()
		}
		// SubagentStart hook: Cursor's third-party import skips SubagentStart
		// events (R-0037), so it stays claude-only.
		if it.Name == "cerne-subagent-context.sh" || strings.HasPrefix(it.Path, "SubagentStart") {
			return []string{string(hooks.ProviderClaude)}
		}
		// All other .claude hooks and wirings: Cursor imports them (R-0037).
		return []string{string(hooks.ProviderClaude), string(hooks.ProviderCursor)}
	default:
		if it.Origin == OriginPath {
			return allProviders()
		}
		return []string{string(hooks.ProviderClaude)}
	}
}

// annotateConsumers stamps the consumers list on every item of every class.
func annotateConsumers(classes []Class, copilot bool) {
	for ci := range classes {
		for ii := range classes[ci].Items {
			classes[ci].Items[ii].Consumers = consumersFor(classes[ci].Items[ii], copilot)
		}
	}
}
