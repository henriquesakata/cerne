#!/usr/bin/env bash
# SessionStart hook installed by `cerne hook install` (F-0139). Its stdout is
# injected as session context, so the agent sees the registration rules before
# touching code. The rules are static but for the GAUNTLET item (F-0364);
# the one call after them (F-0325) is
# local only (cache + backend clone, never SSH/network) and fail-open: it
# exports CERNE_SESSION for the session's own cerne commands and prints the
# session's pending channel messages, or nothing. The hook payload is read
# once, up front, and handed to that call on its stdin. Tool-managed — do not
# edit; fix it upstream in internal/hooks/assets/ and re-run
# `cerne hook install`.
# To turn it off for a process (eval, CI), set CERNE_NO_AGENT_HOOKS=1
# instead of editing it (F-0360).
[ -n "${CERNE_NO_AGENT_HOOKS:-}" ] && exit 0
# A GUI host (Cursor, an IDE opened from the Dock) can run hooks with the
# bare system PATH; look where cerne installs to as well (F-0428).
command -v cerne >/dev/null 2>&1 || PATH="${PATH:+$PATH:}$HOME/go/bin:$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin"
payload=$(cat)
# Cursor runs this script through its import of the .claude hooks and reads
# the hook's stdout as JSON. Its payload carries cursor_version: it gets the
# same rules and live board in its own envelope (F-0428), unless the project's
# .cursor/hooks.json already wires cerne's native sessionStart.
case $payload in
*'"cursor_version"'*)
  grep -qs 'cerne hook session-context' "${CURSOR_PROJECT_DIR:-${CLAUDE_PROJECT_DIR:-.}}/.cursor/hooks.json" && exit 0
  command -v cerne >/dev/null 2>&1 || exit 0
  printf '%s' "$payload" | cerne hook session-context --format cursor 2>/dev/null || true
  exit 0
  ;;
esac
cat <<'RULES'
cerne project rules — make in-flight work visible from minute one:
- START a session with `cerne next` (what to do now, and the lint debts), then a task with its package, `cerne context <id>` (the stage document, the epic and the notes that inform it, within a token budget), not by reading `.docs/CONTEXT.md`: that file is the map of what is live and of the note components — read it when you need the map. Tagging? Reuse a component `cerne tags` lists; a new name near an existing one is refused without --force.
- Picking up a FEATURE? First action: `cerne feature new "<title>" --context "<1-3 sentences of why>"` (lands as a candidate on the kanban; --file spec.md for a one-shot spec). In an agent shell every card creation (feature/bug/epic/task new) needs `--on-behalf "<the human's literal words>"` when a human asked for it, or `--origin agent` when the idea is yours; neither is refused (B-0086). Before writing that spec, read the code the request touches: the Problem cites real file:line and no open question asks what the code already answers. Register it complete in that same act: spec (`--file`), tags (`--facet`/`--component`) and, once the project has a capability taxonomy, capability (`--capability C-…` on `feature new`/`task new`) — `cerne feature approve <id> --check` flags a card without them and the approve refuses it. Advance it as you go: spec -> approve (HUMAN gate), plan -> `cerne feature start`, result -> `cerne feature complete` as soon as the PR is open (never wait for CI or the merge). Abandoned? `cerne feature reject <id> --reason "..."`.
RULES
# The planning item and the DOCS LANGUAGE line come from cerne (F-0475): the
# example speaks the project's documentation language and the line lists its
# forms. With no cerne, or a cerne that fails, the item is printed in the
# neutral form written here and the line is left out.
lang_rules=""
if command -v cerne >/dev/null 2>&1; then
  lang_rules=$(cerne hook lang-rules </dev/null 2>/dev/null) || lang_rules=""
fi
if [ -n "$lang_rules" ]; then
  printf '%s\n' "$lang_rules"
else
  cat <<'PLANNING'
- Picking up an ALREADY-APPROVED feature to plan? First action, before writing the plan: `cerne work start "F-NNNN: <what> — <planning, in the docs language>" --ref F-NNNN` (F-0208) — registers the pickup immediately, closing the blind window before `feature start` (which needs a real plan and so lags). A `cerne push` of the plan auto-opens the same work if you forget. It closes by itself at `cerne feature start` (F-0341).
PLANNING
fi
cat <<'RULES'
- Picking up a NON-feature task (error investigation, review, ops)? First action: `cerne work start "<description>" [--ref F-...]`; when finished, `cerne work done W-... [--note "..."]`. BEFORE delegating an error investigation to another agent, run `cerne work list` — someone may already be on it. (Feature implementations need no manual work entry: `cerne feature start`/`complete` auto-register the activity, F-0146.)
- HANDOFF (F-0368): Passing the baton? `cerne work handoff W-… --next …` and pass only the HO- code. Received an HO-? First action: `cerne handoff take HO-…`. Continuing a card another session started, with no HO- (a fork, an app handoff)? First action: `cerne work start "F-NNNN: <what>" --ref F-NNNN` — it binds your session to the card's cost.
- HUMAN GATES (F-0131): `cerne feature approve` and `cerne feature accept` are human acts by default — write the spec, ask for approval, STOP. Never run them yourself unless the project's .cernerc [gates] explicitly sets auto. Under [gates] accept = "merge" the human's merge of the PR is the acceptance: record it with `cerne feature accept --merged`, and never merge a PR yourself. To be woken when the human acts (F-0460): if your harness wakes you when a background command ends (interactive Claude Code does), leave `cerne feature wait F-… --for approved` (`--for done` after the complete) running in the background, then stop; otherwise just stop.
RULES
# The GAUNTLET item comes from cerne (F-0364): nothing when the project runs in
# light mode ([gates] gauntlet = "off"). Fail-safe toward the Gauntlet: with
# no cerne, or a cerne that fails, the item is printed as written here.
if ! { command -v cerne >/dev/null 2>&1 && cerne hook gauntlet-rule </dev/null 2>/dev/null; }; then
  cat <<'GAUNTLET'
- GAUNTLET (F-0270): every feature is born with a bar and does not close without the loop — the spec's Verification (pt-BR: Verificação) section carries a literal bar line, `Bar:` (pt-BR: `Barra:`), in the project's documentation language (approve refuses it otherwise) — the line you write is a proposal and the human approve is its acceptance, so run `cerne feature approve <id> --check` before asking for it — and `result.md` carries `## Gauntlet` with the judge's quoted verdict (`cerne lint` charges it). Protocol details: cerne-authoring skill.
GAUNTLET
fi
cat <<'RULES'
- DUPLICATE GUARD (F-0147): `feature new`/`work start` refuse a title similar to a live feature or active work, listing the matches — someone already registered it; join that work instead. `--force` only when it is genuinely a distinct problem.
RULES

command -v cerne >/dev/null 2>&1 || exit 0
printf '%s' "$payload" | cerne hook channel --point session-start 2>/dev/null || true
exit 0
