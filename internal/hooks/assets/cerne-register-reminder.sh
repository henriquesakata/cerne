#!/usr/bin/env bash
# SessionStart hook installed by `cerne hook install` (F-0139). Its stdout is
# injected as session context, so the agent sees the registration rules before
# touching code. Deliberately static: it must not depend on the cerne backend
# (SSH/network) being reachable at session start. Tool-managed — do not edit;
# fix it upstream in internal/hooks/assets/ and re-run `cerne hook install`.
cat <<'RULES'
cerne project rules — make in-flight work visible from minute one:
- Picking up a FEATURE? First action: `cerne feature new "<title>" --context "<1-3 sentences of why>"` (lands as a candidate on the kanban; --file spec.md for a one-shot spec). Advance it as you go: spec -> approve (HUMAN gate), plan -> `cerne feature start`, result -> `cerne feature complete`. Abandoned? `cerne feature reject <id> --reason "..."`.
- Picking up a NON-feature task (error investigation, review, ops)? First action: `cerne work start "<description>" [--ref F-...]`; when finished, `cerne work done W-... [--note "..."]`. BEFORE delegating an error investigation to another agent, run `cerne work list` — someone may already be on it. (Feature implementations need no manual work entry: `cerne feature start`/`complete` auto-register the activity, F-0146.)
- HUMAN GATES (F-0131): `cerne feature approve` and `cerne feature accept` are human acts by default — write the spec, ask for approval, STOP. Never run them yourself unless the project's .cernerc [gates] explicitly sets auto.
RULES
