package hooks

import "strings"

// Provider parity matrix (F-0174, criterion 6): the single source of truth
// for which capability each provider install covers. The committed
// docs/provider-parity.md is GENERATED from this table (golden-tested), so
// the published matrix can never drift from what `cerne hook install`
// actually does. Research provenance: R-0035 (Codex), R-0036 (Gemini),
// R-0037 (Cursor).

// ParityRow is one capability line: what each provider install delivers.
// Exported for the harness inventory (F-0251), which renders the same table —
// no second source of truth.
type ParityRow struct {
	Capability string
	Claude     string
	Cursor     string
	Codex      string
	Gemini     string
	Copilot    string
}

// For returns the row's value for one provider ("" for unknown).
func (r ParityRow) For(p Provider) string {
	switch p {
	case ProviderClaude:
		return r.Claude
	case ProviderCursor:
		return r.Cursor
	case ProviderCodex:
		return r.Codex
	case ProviderGemini:
		return r.Gemini
	case ProviderCopilot:
		return r.Copilot
	}
	return ""
}

// Degraded reports whether the value marks a capability the provider surface
// cannot host, with the reason after the marker.
func (r ParityRow) Degraded(p Provider) (bool, string) {
	v := r.For(p)
	if strings.HasPrefix(v, degraded) {
		return true, strings.TrimPrefix(strings.TrimPrefix(v, degraded), ": ")
	}
	return false, ""
}

// GuideCondition is what the provider's official docs say about loading the
// delivered authoring guide (R-0064 Codex, R-0065 Gemini, R-0066 Cursor). It is
// a documented condition, never a claim that the agent loaded it: cerne cannot
// verify that. One source for the install output, the matrix and the docs.
func GuideCondition(p Provider) string {
	switch p {
	case ProviderClaude:
		return "Claude Code loads project skills automatically"
	case ProviderCursor:
		return "Cursor documents project skills as loaded automatically; no setting documented"
	case ProviderCodex:
		return "Codex documents automatic detection of .agents/skills; no setting documented, restart Codex if the skill does not appear"
	case ProviderGemini:
		return "Gemini CLI discovers it at session start only while the skill is enabled (check /skills list, /skills enable cerne-authoring) and asks your consent when the agent activates it"
	case ProviderCopilot:
		return "Copilot documents project skills under .github/skills as available to its CLI, VS Code agent mode and cloud agent; the skill is invoked by the agent when its description matches"
	}
	return ""
}

// ParityRows returns the full F-0174 matrix, in table order.
func ParityRows() []ParityRow {
	out := make([]ParityRow, len(parityTable))
	copy(out, parityTable)
	return out
}

// degraded marks a capability the provider surface cannot host (or that
// awaits live validation) — mirrored by a StatusSkipped result at install.
const degraded = "degraded"

// EveryHarness marks a capability the CLI delivers on its own stdout (F-0426):
// no install, no hook, the same on every provider — never an install gap.
const EveryHarness = "CLI output (every harness)"

// NotApplicable marks a capability that does not exist for a provider — never
// an install gap.
const NotApplicable = "n/a"

var parityTable = []ParityRow{
	{
		Capability: "Static registration rules (floor)",
		Claude:     "SessionStart script (cerne-register-reminder.sh)",
		Cursor:     ".cursor/rules/cerne.mdc (alwaysApply)",
		Codex:      "native AGENTS.md chain — no asset needed",
		Gemini:     "GEMINI.md/context.fileName — plus SessionStart hook",
		Copilot:    ".github/instructions/cerne.instructions.md (applyTo **) — own file, copilot-instructions.md untouched",
	},
	{
		Capability: "Live board at session start",
		Claude:     "SessionStart hook (project-owned or session-context)",
		Cursor:     "sessionStart hook → additional_context",
		Codex:      "SessionStart hook → additionalContext",
		Gemini:     "SessionStart hook → additionalContext",
		Copilot:    "SessionStart hook → top-level additionalContext (.github/hooks/cerne.json)",
	},
	{
		Capability: "Work gate (deny unregistered edits)",
		Claude:     "PreToolUse deny (cerne-work-gate.sh)",
		Cursor:     ".claude PreToolUse deny, run by Cursor's third-party import (on by default; F-0428)",
		Codex:      "PreToolUse deny, matcher Edit|Write",
		Gemini:     "BeforeTool deny, matcher write_file|replace",
		Copilot:    "PreToolUse deny, matcher Edit|Write → top-level permissionDecision (cloud agent reads bash only and treats ask as deny; the gate only denies)",
	},
	{
		Capability: "Session→work binding",
		Claude:     "PostToolUse/Bash (cerne-work-bind.sh)",
		Cursor:     ".claude PostToolUse/Bash, via the third-party import (tool_output, F-0428)",
		Codex:      "PostToolUse, matcher Bash",
		Gemini:     "AfterTool, matcher run_shell_command (--format gemini)",
		Copilot:    "PostToolUse, matcher Bash (tool_result, --format copilot)",
	},
	{
		// F-0426: the guidance rides the command's stdout, no hook needed.
		Capability: "Lifecycle guidance (feature new/start, idea promote)",
		Claude:     EveryHarness,
		Cursor:     EveryHarness,
		Codex:      EveryHarness,
		Gemini:     EveryHarness,
		Copilot:    EveryHarness,
	},
	{
		Capability: "Provenance reminder",
		Claude:     "PostToolUse/Bash (provenance)",
		Cursor:     ".claude PostToolUse/Bash (provenance), via the third-party import → additional_context",
		Codex:      "PostToolUse, matcher Bash",
		Gemini:     degraded + ": AfterTool context envelope pending live validation",
		Copilot:    "PostToolUse/Bash → top-level additionalContext",
	},
	{
		Capability: "Stop closeout",
		Claude:     "Stop hook (cerne-stop-closeout.sh)",
		Cursor:     ".claude Stop hook via the third-party import → followup_message, completed turns only",
		Codex:      "Stop hook",
		Gemini:     degraded + ": AfterAgent forces a retry — deferred",
		Copilot:    degraded + ": the Stop block decision was never exercised live (only the payload was captured) — treat the closeout reminder as unverified",
	},
	{
		Capability: "Handoff code through compaction",
		Claude:     "PreCompact hook (cerne-precompact.sh) + SessionStart source=compact",
		Cursor:     "preCompact hook (silent, records compactions) + first postToolUse additional_context resume (F-0525)",
		Codex:      degraded + ": PreCompact not wired yet",
		Gemini:     degraded + ": no compaction hook",
		Copilot:    degraded + ": preCompact is notification-only",
	},
	{
		Capability: "Notes matching the prompt (B-0020)",
		Claude:     "UserPromptSubmit hook (cerne-prompt-notes.sh) → additionalContext",
		Cursor:     degraded + ": no verified prompt-submit hook that adds context",
		Codex:      "UserPromptSubmit hook (prompt-notes) → additionalContext (F-0491)",
		Gemini:     degraded + ": prompt-submit event not verified here — not wired",
		Copilot:    degraded + ": prompt-submit event not verified here — not wired",
	},
	{
		// F-0457: the documented context channel decides before vs after the tool.
		Capability: "Notes by declared trigger (F-0457)",
		Claude:     "before the tool: PreToolUse/Bash (cerne-tool-notes.sh); edits through the work gate",
		Cursor:     "after the tool: .claude PostToolUse/Bash|Edit|Write (cerne-after-tool-notes.sh), via the third-party import → additional_context",
		Codex:      "before the tool: PreToolUse, matcher Bash; edits through the work gate",
		Gemini:     "after the tool: AfterTool, matcher run_shell_command|write_file|replace → additionalContext (documented envelope, not validated live)",
		Copilot:    "after the tool: PostToolUse, matchers Bash and Edit|Write → top-level additionalContext",
	},
	{
		// B-0054: every contact point the harness documents context on, past
		// session start (the board row), so a session that only talks or only
		// runs shell commands still hears the project channel.
		Capability: "Project channel mid-session (F-0325, B-0054)",
		Claude:     "UserPromptSubmit (cerne-prompt-notes.sh), PreToolUse/Bash (cerne-tool-notes.sh) and the work gate on Edit|Write → additionalContext; Stop reminder for what is addressed and unanswered",
		Cursor:     "after the tool: .claude PostToolUse/Bash|Edit|Write (cerne-after-tool-notes.sh), via the third-party import → additional_context",
		Codex:      "UserPromptSubmit (prompt-notes) and, before the tool, PreToolUse, matchers Bash (tool-notes) and Edit|Write (work gate) → additionalContext",
		Gemini:     "after the tool: AfterTool, matcher run_shell_command|write_file|replace → additionalContext (documented envelope, not validated live)",
		Copilot:    "after the tool: PostToolUse, matchers Bash and Edit|Write → top-level additionalContext",
	},
	{
		// F-0491: a session waiting for its user passes no contact point.
		Capability: "Wake an idle session (F-0491)",
		Claude:     "after a post, one line to the idle session's inbox socket (cross-session messaging, Claude Code 2.1.224 or later) when its last mode is default, acceptEdits, auto or dontAsk; a session in bypassPermissions or an unknown mode holds such a message, so it is not written to and gets it at its next contact point; not on Windows, whose inbox needs the session's token",
		Cursor:     degraded + ": no documented way to open a turn in an idle session — the message waits for the next contact point",
		Codex:      degraded + ": only the app-server connection's owner starts a turn, and cerne does not own the TUI's — the message waits for the next contact point",
		Gemini:     degraded + ": no documented way to open a turn in an idle session — the message waits for the next contact point",
		Copilot:    degraded + ": no documented way to open a turn in an idle session — the message waits for the next contact point",
	},
	{
		Capability: "CERNE_ACTOR=agent (human gates)",
		Claude:     "settings.json env block + CLAUDECODE=1 on every shell Claude Code runs (B-0030) — no install",
		Cursor:     "CURSOR_AGENT=1 on Cursor's agent shells (seen in 3.25.6, undocumented) — no install",
		Codex:      "shell_environment_policy.set in .codex/config.toml, or CODEX_THREAD_ID (non-empty) on Codex's tool shells — no install (B-0051)",
		Gemini:     ".gemini/.env",
		Copilot:    "env block on each hook + COPILOT_CLI=1 on the bash tool shell — verified on Copilot CLI 1.0.112 only; VS Code agent mode and the cloud agent unverified — no install",
	},
	{
		Capability: "Document author (CERNE_PROVIDER, F-0375)",
		Claude:     "no declaration — the model is read from the session transcript",
		Cursor:     "no declaration — the hooks record each session's model from the hook payload (model_id, else model), and a write from Cursor's agent shell is provider cursor with that model; CURSOR_CONVERSATION_ID in the agent shell attributes the write to that conversation when its cursor-sessions record is fresh (F-0525); no model when the Cursor sessions in play disagree (F-0513)",
		Codex:      "CERNE_PROVIDER=openai in shell_environment_policy.set; no model exposed",
		Gemini:     "CERNE_PROVIDER=google in .gemini/.env; no model exposed",
		Copilot:    degraded + ": CERNE_PROVIDER=copilot reaches the hook processes only, not the bash tool shell — no model exposed",
	},
	{
		Capability: "Subagents with live board",
		Claude:     "SubagentStart hook (cerne-subagent-context.sh)",
		Cursor:     degraded + ": subagentStart has no additional_context",
		Codex:      degraded + ": SubagentStart envelope pending live validation",
		Gemini:     degraded + ": subagents are a preview feature",
		Copilot:    degraded + ": no subagent hook wired",
	},
	{
		Capability: "Authoring guide",
		Claude:     "delivered to .claude/skills/cerne-authoring/ — " + GuideCondition(ProviderClaude),
		Cursor:     "delivered to .cursor/skills/cerne-authoring/ — " + GuideCondition(ProviderCursor),
		Codex:      "delivered to .agents/skills/cerne-authoring/ + one-line pointer in AGENTS.md — " + GuideCondition(ProviderCodex),
		Gemini:     "delivered to .gemini/skills/cerne-authoring/ + one-line pointer in GEMINI.md — " + GuideCondition(ProviderGemini),
		Copilot:    "delivered to .github/skills/cerne-authoring/ + .github/instructions/cerne.instructions.md — " + GuideCondition(ProviderCopilot),
	},
	{
		Capability: "Double execution with the .claude hooks",
		Claude:     NotApplicable,
		Cursor:     NotApplicable,
		Codex:      NotApplicable,
		Gemini:     NotApplicable,
		Copilot:    degraded + ": Copilot CLI also runs the cerne hooks of .claude/settings.json (probed) — it ignores their Claude-format output, so only side effects repeat (binding, sign of life)",
	},
	{
		Capability: "Trust prerequisite",
		Claude:     "none",
		Cursor:     "trusted workspaces",
		Codex:      "projects.<path>.trust_level + per-hook trust review",
		Gemini:     "trusted folders",
		Copilot:    "trusted folder: in prompt mode an untrusted folder skips the repository hooks (probed); the cloud agent needs none",
	},
	{
		Capability: "Agent sandbox: cache and local backend writable (F-0474, F-0512)",
		Claude:     NotApplicable,
		Cursor:     ".cursor/sandbox.json additionalReadwritePaths with ~/ paths, merged key by key, written only by an explicit hook install (never by a renewal); the cache needs a project_id that is one folder name, a local backend must be a git repository that is neither the home nor holds it; a machine-specific folder (CERNE_CACHE_DIR, XDG_CACHE_HOME, a backend outside the home) or invalid JSON writes nothing and prints the block for ~/.cursor/sandbox.json",
		Codex:      ".codex/config.toml profile cerne extending :workspace, with ~/ paths; a machine-specific folder prints the snippet for ~/.codex/config.toml",
		Gemini:     NotApplicable,
		Copilot:    NotApplicable,
	},
	{
		Capability: "Network for pull and push (--network, F-0484, F-0512)",
		Claude:     NotApplicable,
		Cursor:     "--network adds only the remote backend's host to networkPolicy.allow in .cursor/sandbox.json: no default, and nothing is written when a deny entry could match the host (exact, *, *.domain, an IP or CIDR); a local backend opens nothing",
		Codex:      "--network sets enabled = true under [permissions.cerne.network]: the whole network, Codex has no domain list",
		Gemini:     NotApplicable,
		Copilot:    NotApplicable,
	},
	{
		Capability: "Read-only MCP server (cerne mcp)",
		Claude:     NotApplicable,
		Cursor:     ".cursor/mcp.json mcpServers.cerne (stdio, cerne mcp), written only by an explicit hook install; Cursor does not read the root .mcp.json",
		Codex:      NotApplicable,
		Gemini:     NotApplicable,
		Copilot:    "root .mcp.json mcpServers.cerne with the explicit tools list; the cloud agent needs the block pasted in the repository settings",
	},
}

// ParityMatrix renders the provider parity matrix as the markdown document
// committed at docs/provider-parity.md.
func ParityMatrix() string {
	var b strings.Builder
	b.WriteString(`# Provider parity matrix

<!-- GENERATED from internal/hooks/parity.go (F-0174) — do not edit by hand.
     A golden test pins this file to the table the installer actually uses. -->

What ` + "`cerne hook install`" + ` (and, for the authoring guide, ` + "`cerne skill install`" + `) delivers per provider. Capabilities marked
**degraded** are limitations of the provider's extension surface (or await
live validation) and are reported as ` + "`skipped`" + ` lines, with the manual
step, in the install output. Research provenance: R-0035 (Codex), R-0036
(Gemini), R-0037 (Cursor), R-0146 and R-0147 (Cursor sandbox and MCP), R-0101 (Copilot hooks); the authoring-guide paths: R-0064 (Codex), R-0065 (Gemini), R-0066 (Cursor), in the cerne backend.

| Capability | Claude Code | Cursor | Codex CLI | Gemini CLI | GitHub Copilot |
|---|---|---|---|---|---|
`)
	for _, r := range parityTable {
		b.WriteString("| " + r.Capability + " | " + r.Claude + " | " + r.Cursor + " | " + r.Codex + " | " + r.Gemini + " | " + r.Copilot + " |\n")
	}
	b.WriteString(`
"Delivered" means the file is in place; cerne cannot verify that the agent loads it,
so the row states the load condition from the agent's own docs. An agent whose config
folder is absent gets a ` + "`not detected`" + ` line from ` + "`cerne skill install`" + `, with the
step to run first.

Install: ` + "`cerne hook install`" + ` (claude + auto-detected providers by
config dir) or ` + "`cerne hook install --provider cursor,codex,gemini,copilot`" + `.
`)
	return b.String()
}
