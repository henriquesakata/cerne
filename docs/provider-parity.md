# Provider parity matrix

<!-- GENERATED from internal/hooks/parity.go (F-0174) — do not edit by hand.
     A golden test pins this file to the table the installer actually uses. -->

What `cerne hook install` (and, for the authoring guide, `cerne skill install`) delivers per provider. Capabilities marked
**degraded** are limitations of the provider's extension surface (or await
live validation) and are reported as `skipped` lines, with the manual
step, in the install output. Research provenance: R-0035 (Codex), R-0036
(Gemini), R-0037 (Cursor), R-0101 (Copilot hooks); the authoring-guide paths: R-0064 (Codex), R-0065 (Gemini), R-0066 (Cursor), in the cerne backend.

| Capability | Claude Code | Cursor | Codex CLI | Gemini CLI | GitHub Copilot |
|---|---|---|---|---|---|
| Static registration rules (floor) | SessionStart script (cerne-register-reminder.sh) | .cursor/rules/cerne.mdc (alwaysApply) | native AGENTS.md chain — no asset needed | GEMINI.md/context.fileName — plus SessionStart hook | .github/instructions/cerne.instructions.md (applyTo **) — own file, copilot-instructions.md untouched |
| Live board at session start | SessionStart hook (project-owned or session-context) | sessionStart hook → additional_context | SessionStart hook → additionalContext | SessionStart hook → additionalContext | SessionStart hook → top-level additionalContext (.github/hooks/cerne.json) |
| Work gate (deny unregistered edits) | PreToolUse deny (cerne-work-gate.sh) | .claude PreToolUse deny, run by Cursor's third-party import (on by default; F-0428) | PreToolUse deny, matcher Edit|Write | BeforeTool deny, matcher write_file|replace | PreToolUse deny, matcher Edit|Write → top-level permissionDecision (cloud agent reads bash only and treats ask as deny; the gate only denies) |
| Session→work binding | PostToolUse/Bash (cerne-work-bind.sh) | .claude PostToolUse/Bash, via the third-party import (tool_output, F-0428) | PostToolUse, matcher Bash | AfterTool, matcher run_shell_command (--format gemini) | PostToolUse, matcher Bash (tool_result, --format copilot) |
| Lifecycle guidance (feature new/start, idea promote) | CLI output (every harness) | CLI output (every harness) | CLI output (every harness) | CLI output (every harness) | CLI output (every harness) |
| Provenance reminder | PostToolUse/Bash (provenance) | .claude PostToolUse/Bash (provenance), via the third-party import → additional_context | PostToolUse, matcher Bash | degraded: AfterTool context envelope pending live validation | PostToolUse/Bash → top-level additionalContext |
| Stop closeout | Stop hook (cerne-stop-closeout.sh) | .claude Stop hook via the third-party import → followup_message, completed turns only | Stop hook | degraded: AfterAgent forces a retry — deferred | degraded: the Stop block decision was never exercised live (only the payload was captured) — treat the closeout reminder as unverified |
| Handoff code through compaction | PreCompact hook (cerne-precompact.sh) + SessionStart source=compact | degraded: PreCompact not wired yet | degraded: PreCompact not wired yet | degraded: no compaction hook | degraded: preCompact is notification-only |
| Notes matching the prompt (B-0020) | UserPromptSubmit hook (cerne-prompt-notes.sh) → additionalContext | degraded: no verified prompt-submit hook that adds context | degraded: prompt-submit event not verified here — not wired | degraded: prompt-submit event not verified here — not wired | degraded: prompt-submit event not verified here — not wired |
| Notes by declared trigger (F-0457) | before the tool: PreToolUse/Bash (cerne-tool-notes.sh); edits through the work gate | after the tool: .claude PostToolUse/Bash|Edit|Write (cerne-after-tool-notes.sh), via the third-party import → additional_context | before the tool: PreToolUse, matcher Bash; edits through the work gate | after the tool: AfterTool, matcher run_shell_command|write_file|replace → additionalContext (documented envelope, not validated live) | after the tool: PostToolUse, matchers Bash and Edit|Write → top-level additionalContext |
| CERNE_ACTOR=agent (human gates) | settings.json env block + CLAUDECODE=1 on every shell Claude Code runs (B-0030) — no install | CURSOR_AGENT=1 on Cursor's agent shells (seen in 3.25.6, undocumented) — no install | shell_environment_policy.set in .codex/config.toml | .gemini/.env | env block on each hook + COPILOT_CLI=1 on the bash tool shell — verified on Copilot CLI 1.0.112 only; VS Code agent mode and the cloud agent unverified — no install |
| Document author (CERNE_PROVIDER, F-0375) | no declaration — the model is read from the session transcript | degraded: no per-project session env surface — recorded as unknown | CERNE_PROVIDER=openai in shell_environment_policy.set; no model exposed | CERNE_PROVIDER=google in .gemini/.env; no model exposed | degraded: CERNE_PROVIDER=copilot reaches the hook processes only, not the bash tool shell — no model exposed |
| Subagents with live board | SubagentStart hook (cerne-subagent-context.sh) | degraded: subagentStart has no additional_context | degraded: SubagentStart envelope pending live validation | degraded: subagents are a preview feature | degraded: no subagent hook wired |
| Authoring guide | delivered to .claude/skills/cerne-authoring/ — Claude Code loads project skills automatically | delivered to .cursor/skills/cerne-authoring/ — Cursor documents project skills as loaded automatically; no setting documented | delivered to .agents/skills/cerne-authoring/ + one-line pointer in AGENTS.md — Codex documents automatic detection of .agents/skills; no setting documented, restart Codex if the skill does not appear | delivered to .gemini/skills/cerne-authoring/ + one-line pointer in GEMINI.md — Gemini CLI discovers it at session start only while the skill is enabled (check /skills list, /skills enable cerne-authoring) and asks your consent when the agent activates it | delivered to .github/skills/cerne-authoring/ + .github/instructions/cerne.instructions.md — Copilot documents project skills under .github/skills as available to its CLI, VS Code agent mode and cloud agent; the skill is invoked by the agent when its description matches |
| Double execution with the .claude hooks | n/a | n/a | n/a | n/a | degraded: Copilot CLI also runs the cerne hooks of .claude/settings.json (probed) — it ignores their Claude-format output, so only side effects repeat (binding, sign of life) |
| Trust prerequisite | none | trusted workspaces | projects.<path>.trust_level + per-hook trust review | trusted folders | trusted folder: in prompt mode an untrusted folder skips the repository hooks (probed); the cloud agent needs none |

"Delivered" means the file is in place; cerne cannot verify that the agent loads it,
so the row states the load condition from the agent's own docs. An agent whose config
folder is absent gets a `not detected` line from `cerne skill install`, with the
step to run first.

Install: `cerne hook install` (claude + auto-detected providers by
config dir) or `cerne hook install --provider cursor,codex,gemini,copilot`.
