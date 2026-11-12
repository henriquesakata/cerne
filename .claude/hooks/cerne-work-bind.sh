#!/usr/bin/env bash
# PostToolUse/Bash hook installed by `cerne hook install` (F-0151). Pipes the
# hook payload to `cerne hook bind-work`, which binds this Claude Code session
# to the W-/F- id it just registered via `cerne work start`/`cerne feature
# start` — the work gate then recognizes the session. All parsing and decisions
# live in the Go subcommand; this wrapper only degrades silently (exit 0) when
# the cerne binary is absent, so a broken install never breaks a tool call.
# Tool-managed — do not edit; fix it upstream in internal/hooks/assets/ and
# re-run `cerne hook install`.
# To turn it off for a process (eval, CI), set CERNE_NO_AGENT_HOOKS=1
# instead of editing it (F-0360).
[ -n "${CERNE_NO_AGENT_HOOKS:-}" ] && exit 0

command -v cerne >/dev/null 2>&1 || exit 0
cerne hook bind-work 2>/dev/null || true
exit 0
