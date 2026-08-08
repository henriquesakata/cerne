#!/usr/bin/env bash
# SubagentStart hook installed by `cerne hook install` (F-0150). SessionStart
# does not fire inside subagents, so every subagent would otherwise start blind
# to cerne; this hook injects the condensed cerne rules plus the live work list
# via additionalContext at spawn. It degrades silently (exit 0, no output) when
# the cerne binary is absent or the directory is not a cerne project — it must
# never break a subagent spawn. Tool-managed — do not edit; fix it upstream in
# internal/hooks/assets/ and re-run `cerne hook install`.

command -v cerne >/dev/null 2>&1 || exit 0
worklist=$(cerne work list 2>/dev/null) || exit 0

rules=$(cat <<'RULES'
cerne project rules (subagent context — the main-session rules do not reach subagents):
- cerne is the documentation source of truth here and its use is MANDATORY.
- BEFORE acting, read the active work below: if your task overlaps an entry, join or coordinate with that work instead of duplicating it (the duplicate guard, F-0147, refuses similar titles).
- Picking up a NON-feature task (error investigation, review, ops)? First action: `cerne work start "<description>" [--ref F-...]`; when finished, `cerne work done W-... [--note "..."]`.
- The feature lifecycle belongs to the parent flow (`cerne feature ...`); NEVER run `cerne feature approve`/`accept` yourself — those are human gates (F-0131).

ACTIVE WORK RIGHT NOW (cerne work list):
RULES
)

ctx="$rules
$worklist"

# JSON-escape with bash parameter expansion only — no jq dependency.
ctx=${ctx//\\/\\\\}
ctx=${ctx//\"/\\\"}
ctx=${ctx//$'\r'/}
ctx=${ctx//$'\t'/\\t}
ctx=${ctx//$'\n'/\\n}

printf '{"hookSpecificOutput":{"hookEventName":"SubagentStart","additionalContext":"%s"}}\n' "$ctx"
