---
description: cerne project rules — work registration discipline
applyTo: "**"
---

cerne project rules — make in-flight work visible from minute one:
- Picking up a FEATURE? First action: `cerne feature new "<title>" --context "<1-3 sentences of why>"` (lands as a candidate on the kanban; --file spec.md for a one-shot spec). Register it complete in that same act: spec (`--file`), tags (`--facet`/`--component`) and, once the project has a capability taxonomy, capability (`cerne feature capability <id> C-…`) — `cerne feature approve <id> --check` flags a card without them and the approve refuses it. Advance it as you go: spec -> approve (HUMAN gate), plan -> `cerne feature start`, result -> `cerne feature complete` as soon as the PR is open (never wait for CI or the merge). Abandoned? `cerne feature reject <id> --reason "..."`.
- Picking up an ALREADY-APPROVED feature to plan? First action, before writing the plan: `cerne work start "F-NNNN: <what> — planejamento" --ref F-NNNN` (F-0208) — registers the pickup immediately, closing the blind window before `feature start` (which needs a real plan and so lags). A `cerne push` of the plan auto-opens the same work if you forget. It closes by itself at `cerne feature start` (F-0341).
- Picking up a NON-feature task (error investigation, review, ops)? First action: `cerne work start "<description>" [--ref F-...]`; when finished, `cerne work done W-... [--note "..."]`. BEFORE delegating an error investigation to another agent, run `cerne work list` — someone may already be on it.
- HANDOFF (F-0368): Passing the baton? `cerne work handoff W-… --next …` and pass only the HO- code. Received an HO-? First action: `cerne handoff take HO-…`.
- HUMAN GATES (F-0131): `cerne feature approve` and `cerne feature accept` are human acts by default — write the spec, ask for approval, STOP.
- GAUNTLET (F-0270): every feature is born with a bar and does not close without the loop — the spec's Verification (pt-BR: Verificação) section carries a literal bar line, `Bar:` (pt-BR: `Barra:`), in the project's documentation language (approve refuses it otherwise) — the line you write is a proposal and the human approve is its acceptance, so run `cerne feature approve <id> --check` before asking for it — and `result.md` carries `## Gauntlet` with the judge's quoted verdict (`cerne lint` charges it). Protocol details: cerne-authoring skill. (skip it when .cernerc sets [gates] gauntlet = "off")
- DUPLICATE GUARD (F-0147): `feature new`/`work start` refuse a title similar to a live feature or active work — join that work instead; --force only for a genuinely distinct problem.

cerne authoring guide: before creating or managing cerne features, decisions or references, read `.github/skills/cerne-authoring/SKILL.md`.
