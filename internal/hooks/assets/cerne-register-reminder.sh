#!/usr/bin/env bash
# SessionStart hook installed by `cerne hook install` (F-0139). Its stdout is
# injected as session context, so the agent sees the registration rules before
# touching code. The rules are static; the one call after them (F-0325) is
# local only (cache + backend clone, never SSH/network) and fail-open: it
# exports CERNE_SESSION for the session's own cerne commands and prints the
# session's pending channel messages, or nothing. The heredoc does not read
# stdin, so the hook payload reaches that call intact. Tool-managed — do not
# edit; fix it upstream in internal/hooks/assets/ and re-run
# `cerne hook install`.
# To turn it off for a process (eval, CI), set CERNE_NO_AGENT_HOOKS=1
# instead of editing it (F-0360).
[ -n "${CERNE_NO_AGENT_HOOKS:-}" ] && exit 0
cat <<'RULES'
cerne project rules — make in-flight work visible from minute one:
- Picking up a FEATURE? First action: `cerne feature new "<title>" --context "<1-3 sentences of why>"` (lands as a candidate on the kanban; --file spec.md for a one-shot spec). Register it complete in that same act: spec (`--file`), tags (`--facet`/`--component`) and, once the project has a capability taxonomy, capability (`cerne feature capability <id> C-…`) — `cerne feature approve <id> --check` flags a card without them and the approve refuses it. Advance it as you go: spec -> approve (HUMAN gate), plan -> `cerne feature start`, result -> `cerne feature complete`. Abandoned? `cerne feature reject <id> --reason "..."`.
- Picking up an ALREADY-APPROVED feature to plan? First action, before writing the plan: `cerne work start "F-NNNN: <what> — planejamento" --ref F-NNNN` (F-0208) — registers the pickup immediately, closing the blind window before `feature start` (which needs a real plan and so lags). A `cerne push` of the plan auto-opens the same work if you forget. It closes by itself at `cerne feature start` (F-0341).
- Picking up a NON-feature task (error investigation, review, ops)? First action: `cerne work start "<description>" [--ref F-...]`; when finished, `cerne work done W-... [--note "..."]`. BEFORE delegating an error investigation to another agent, run `cerne work list` — someone may already be on it. (Feature implementations need no manual work entry: `cerne feature start`/`complete` auto-register the activity, F-0146.)
- HUMAN GATES (F-0131): `cerne feature approve` and `cerne feature accept` are human acts by default — write the spec, ask for approval, STOP. Never run them yourself unless the project's .cernerc [gates] explicitly sets auto.
- GAUNTLET (F-0545): every feature is born with a bar and does not close without the loop — the spec's Verificação carries a literal `Barra:` line (approve refuses it otherwise) — the line you write is a proposal and the human approve is its acceptance, so run `cerne feature approve <id> --check` before asking for it — and `result.md` carries `## Gauntlet` with the judge's quoted verdict (`cerne lint` charges it). Protocol details: cerne-authoring skill.
- DUPLICATE GUARD (F-0147): `feature new`/`work start` refuse a title similar to a live feature or active work, listing the matches — someone already registered it; join that work instead. `--force` only when it is genuinely a distinct problem.
RULES

command -v cerne >/dev/null 2>&1 || exit 0
cerne hook channel --point session-start 2>/dev/null || true
exit 0
