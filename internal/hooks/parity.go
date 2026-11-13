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

var parityTable = []ParityRow{
	{
		Capability: "Static registration rules (floor)",
		Claude:     "SessionStart script (cerne-register-reminder.sh)",
		Cursor:     ".cursor/rules/cerne.mdc (alwaysApply)",
		Codex:      "native AGENTS.md chain — no asset needed",
		Gemini:     "GEMINI.md/context.fileName — plus SessionStart hook",
	},
	{
		Capability: "Live board at session start",
		Claude:     "SessionStart hook (project-owned or session-context)",
		Cursor:     "sessionStart hook → additional_context",
		Codex:      "SessionStart hook → additionalContext",
		Gemini:     "SessionStart hook → additionalContext",
	},
	{
		Capability: "Work gate (deny unregistered edits)",
		Claude:     "PreToolUse deny (cerne-work-gate.sh)",
		Cursor:     degraded + ": no documented session key — enable the third-party toggle to reuse the .claude hooks",
		Codex:      "PreToolUse deny, matcher Edit|Write",
		Gemini:     "BeforeTool deny, matcher write_file|replace",
	},
	{
		Capability: "Session→work binding",
		Claude:     "PostToolUse/Bash (cerne-work-bind.sh)",
		Cursor:     degraded + ": depends on the gate route above",
		Codex:      "PostToolUse, matcher Bash",
		Gemini:     "AfterTool, matcher run_shell_command (--format gemini)",
	},
	{
		Capability: "Lifecycle + provenance reminders",
		Claude:     "PostToolUse/Bash (lifecycle-reminder, provenance)",
		Cursor:     degraded + ": via third-party toggle",
		Codex:      "PostToolUse, matcher Bash",
		Gemini:     degraded + ": AfterTool context envelope pending live validation",
	},
	{
		Capability: "Stop closeout",
		Claude:     "Stop hook (cerne-stop-closeout.sh)",
		Cursor:     degraded + ": stop followup_message auto-submits — deferred",
		Codex:      "Stop hook",
		Gemini:     degraded + ": AfterAgent forces a retry — deferred",
	},
	{
		Capability: "Handoff code through compaction",
		Claude:     "PreCompact hook (cerne-precompact.sh) + SessionStart source=compact",
		Cursor:     degraded + ": PreCompact not wired yet",
		Codex:      degraded + ": PreCompact not wired yet",
		Gemini:     degraded + ": no compaction hook",
	},
	{
		Capability: "CERNE_ACTOR=agent (human gates)",
		Claude:     "settings.json env block",
		Cursor:     degraded + ": no per-project session env surface",
		Codex:      "shell_environment_policy.set in .codex/config.toml",
		Gemini:     ".gemini/.env",
	},
	{
		Capability: "Document author (CERNE_PROVIDER, F-0375)",
		Claude:     "no declaration — the model is read from the session transcript",
		Cursor:     degraded + ": no per-project session env surface — recorded as unknown",
		Codex:      "CERNE_PROVIDER=openai in shell_environment_policy.set; no model exposed",
		Gemini:     "CERNE_PROVIDER=google in .gemini/.env; no model exposed",
	},
	{
		Capability: "Subagents with live board",
		Claude:     "SubagentStart hook (cerne-subagent-context.sh)",
		Cursor:     degraded + ": subagentStart has no additional_context",
		Codex:      degraded + ": SubagentStart envelope pending live validation",
		Gemini:     degraded + ": subagents are a preview feature",
	},
	{
		Capability: "Authoring guide",
		Claude:     "delivered to .claude/skills/cerne-authoring/ — " + GuideCondition(ProviderClaude),
		Cursor:     "delivered to .cursor/skills/cerne-authoring/ — " + GuideCondition(ProviderCursor),
		Codex:      "delivered to .agents/skills/cerne-authoring/ + one-line pointer in AGENTS.md — " + GuideCondition(ProviderCodex),
		Gemini:     "delivered to .gemini/skills/cerne-authoring/ + one-line pointer in GEMINI.md — " + GuideCondition(ProviderGemini),
	},
	{
		Capability: "Trust prerequisite",
		Claude:     "none",
		Cursor:     "trusted workspaces",
		Codex:      "projects.<path>.trust_level + per-hook trust review",
		Gemini:     "trusted folders",
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
(Gemini), R-0037 (Cursor); the authoring-guide paths: R-0064 (Codex), R-0065 (Gemini), R-0066 (Cursor), in the cerne backend.

| Capability | Claude Code | Cursor | Codex CLI | Gemini CLI |
|---|---|---|---|---|
`)
	for _, r := range parityTable {
		b.WriteString("| " + r.Capability + " | " + r.Claude + " | " + r.Cursor + " | " + r.Codex + " | " + r.Gemini + " |\n")
	}
	b.WriteString(`
"Delivered" means the file is in place; cerne cannot verify that the agent loads it,
so the row states the load condition from the agent's own docs. An agent whose config
folder is absent gets a ` + "`not detected`" + ` line from ` + "`cerne skill install`" + `, with the
step to run first.

Install: ` + "`cerne hook install`" + ` (claude + auto-detected providers by
config dir) or ` + "`cerne hook install --provider cursor,codex,gemini`" + `.
`)
	return b.String()
}
