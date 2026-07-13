# Provider parity matrix

<!-- GENERATED from internal/hooks/parity.go (F-0174) — do not edit by hand.
     A golden test pins this file to the table the installer actually uses. -->

What `cerne hook install` delivers per provider. Capabilities marked
**degraded** are limitations of the provider's extension surface (or await
live validation) and are reported as `skipped` lines, with the manual
step, in the install output. Research provenance: R-0035 (Codex), R-0036
(Gemini), R-0037 (Cursor) in the cerne backend.

| Capability | Claude Code | Cursor | Codex CLI | Gemini CLI |
|---|---|---|---|---|
| Static registration rules (floor) | SessionStart script (cerne-register-reminder.sh) | .cursor/rules/cerne.mdc (alwaysApply) | native AGENTS.md chain — no asset needed | GEMINI.md/context.fileName — plus SessionStart hook |
| Live board at session start | SessionStart hook (project-owned or session-context) | sessionStart hook → additional_context | SessionStart hook → additionalContext | SessionStart hook → additionalContext |
| Work gate (deny unregistered edits) | PreToolUse deny (cerne-work-gate.sh) | degraded: no documented session key — enable the third-party toggle to reuse the .claude hooks | PreToolUse deny, matcher Edit|Write | BeforeTool deny, matcher write_file|replace |
| Session→work binding | PostToolUse/Bash (cerne-work-bind.sh) | degraded: depends on the gate route above | PostToolUse, matcher Bash | AfterTool, matcher run_shell_command (--format gemini) |
| Lifecycle + provenance reminders | PostToolUse/Bash (lifecycle-reminder, provenance) | degraded: via third-party toggle | PostToolUse, matcher Bash | degraded: AfterTool context envelope pending live validation |
| Stop closeout | Stop hook (cerne-stop-closeout.sh) | degraded: stop followup_message auto-submits — deferred | Stop hook | degraded: AfterAgent forces a retry — deferred |
| CERNE_ACTOR=agent (human gates) | settings.json env block | degraded: no per-project session env surface | shell_environment_policy.set in .codex/config.toml | .gemini/.env |
| Subagents with live board | SubagentStart hook (cerne-subagent-context.sh) | degraded: subagentStart has no additional_context | degraded: SubagentStart envelope pending live validation | degraded: subagents are a preview feature |
| Trust prerequisite | none | trusted workspaces | projects.<path>.trust_level + per-hook trust review | trusted folders |

Install: `cerne hook install` (claude + auto-detected providers by
config dir) or `cerne hook install --provider cursor,codex,gemini`.
