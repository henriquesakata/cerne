---
name: cerne-authoring
description: "Use this whenever you create, plan, finish, or manage a feature, decision (ADR), runbook, note (durable team knowledge), or external reference in a project guarded by cerne. cerne is the documentation source of truth — author through it (cerne feature new -> pull -> push -> approve/start/complete), never as loose docs/case folders. Read current state from .docs/CONTEXT.md and `cerne status`. Route durable team knowledge an agent acquires to `cerne note new`, not the local Claude Code memory."
---

# Cerne-Native Authoring

## Overview

In a cerne-managed project the living documentation is owned by **cerne**: it holds the lifecycle of **features**, **decisions** (ADRs), **runbooks**, **notes** (durable team knowledge) and **references**, and is the source of truth for the current state. The backend (a git repo) is pinned by the committed `docs.lock`; `.cernerc` wires it. You author documents under `.docs/current/…` and push them back through cerne — you do **not** hand-create folders of loose markdown for tracked work.

This skill is the **mechanics and lifecycle**. The *content* of each document (the actual spec/plan/result prose) comes from your normal process. Read current state from `.docs/CONTEXT.md` and `cerne status` (or `cerne brief` for the cheapest whole-project map — every id + state + title, no paths; `--json` too — ideal to prime on the project, then `cerne show` only the ids you need); reach superseded/rejected history with `cerne search "<q>"` / `cerne ask "<q>"`. To open **one entity by its id** — any kind: feature, decision, reference, note, runbook, historical — run **`cerne show <id>`** (`--json` for machine output). It reads straight from the backend store, so it works even for implemented features that are not materialized in `.docs/` — use it instead of guessing a command like `cerne feature show`, which does not exist.

## When to use

- **The moment you pick up a task** — register it first with `cerne feature new` so in-flight work shows on the board from the start, not after it is done. Also for new decisions (ADRs) or registering an external source that informed your work.
- Moving a feature through its lifecycle (approve / start / complete).
- Any moment you would otherwise hand-create a tracked "case" or "feature" doc folder — don't; use this loop.

## Feature loop

cerne mints the canonical `F-NNNN` id and gates each transition on a real document.

**Register at the start, not the end.** The first action when you pick up a task is step 1 below — run it *before* you start building, so the work is visible on the kanban (as a candidate) from minute one instead of an agent running in the dark. The spec/plan/result you write then carry it rightward across the board; if the work is dropped, `reject` it. Creating the feature is cheap and self-limiting — you only mint one when real work actually starts, so the board never fills with speculative ideas nobody is working on.

1. **Create** — `cerne feature new "<title>" [--module <module>] [--parent F-NNNN]` → candidate.
   - `--module` assigns the subsystem when the project declares modules; omit for cross-cutting work.
   - `--parent F-NNNN` makes this feature decompose an epic. See **Epics**.
2. **Spec** — `cerne pull`, author `.docs/current/features/F-NNNN-<slug>/spec.md` (keep the `cerne_id` frontmatter), `cerne push`, then `cerne feature approve F-NNNN` (gate: a real spec, not the stub).
3. **Plan** — `cerne feature approve` scaffolds `plan.md` from a canonical template (or run `cerne plan draft F-NNNN`); fill it in, `cerne push`, then `cerne feature start F-NNNN` (gate: a real plan, not the stub).
4. **Implement** — build it per your project's stack and conventions; add tests; review.
5. **Result** — `cerne feature start` scaffolds `result.md` from a canonical template (or run `cerne result draft F-NNNN`); fill it as you finish, `cerne push`, then `cerne feature complete F-NNNN` (gate: a real result, not the stub). No manual "Done" status, no index edits — `cerne feature complete` is the source of truth.

Terminal transitions when work is dropped or replaced: `cerne feature reject F-NNNN --reason "…"`; `cerne feature substituir F-NNNN --by F-MMMM --reason "…"`.

## Spec structure (the template cerne scaffolds)

cerne scaffolds a new feature's `spec.md` from a canonical template (the same one `cerne spec draft` asks the model to fill). Fill each section; the lifecycle gates treat the empty scaffold as a stub until you do. Sections, **in order**:

1. **Problema** — what is missing or broken **today**, grounded in real code (`file.go:line`); why now; the cost of not fixing it. Problem only — no solution yet.
2. **Objetivo e critérios de aceitação** — one objective sentence, then **numbered, testable** criteria (1 criterion : 1 test). For behaviour, use EARS — *"QUANDO `<condição>` O SISTEMA DEVE `<comportamento>`"*; structural invariants as a direct, measurable assertion.
3. **Decisões do design** — a `| Fork | Decisão | Razão |` table; record the rejected alternatives, not just the chosen path.
4. **Desenho** — by module/package, grounded in real files, with explicit contracts (signatures, schemas, endpoints, events). Include the *how* — don't strip it out.
5. **Verificação** — the test plan (TDD), 1:1 with the acceptance criteria.
6. **Fora de escopo** — what you will **not** do now, and why (or what becomes a follow-up).

**Ordering is deliberate, not cosmetic.** Problema and the acceptance criteria sit at the top and Verificação near the end, so the most decision-bearing, testable content lands where long-context models attend best (primacy/recency) rather than buried in the middle. Keep it dense — a bloated spec hurts more than it helps.

**Mark ambiguity, don't guess.** Wherever something is unresolved, leave `[ESCLARECER: pergunta específica]` instead of a plausible-but-maybe-wrong guess, and resolve every marker before `cerne feature approve`.

The evidence behind this structure (lost-in-the-middle / primacy-recency, EARS, ADR, spec-driven development) is registered in the backend as external references — `cerne ask` cites them.

## Plan structure (the template cerne scaffolds)

cerne scaffolds `plan.md` from a canonical template when you **approve** a feature (the same one `cerne plan draft` asks the model to fill) — the execution counterpart of the spec. A plan is the bridge from an approved spec to code: it says **how** to build, never re-stating **what** (link the spec, don't repeat it — context is finite). **Right-size it:** a trivial change is a single task, and sections that don't apply collapse to one line — don't inflate a small plan with ceremony. Sections, **in order**:

1. **Estratégia** — the approach in 2-3 sentences and how the work is sliced (single MR or N phases) and why. Point at the spec; don't duplicate it.
2. **Sequência de tarefas** — small, **ordered** tasks, each leaving the repo green. Per task: real file paths (create/edit/delete), **test first** (TDD) then the minimal implementation, a **verification** command (→ expected output) **citing the spec criterion number** it satisfies, and the exact Conventional commit message. Mark `[paralela]` when a task is independent.
3. **Dependências e ordem** — what unblocks what (the real order, not just a list), which tasks are parallel-safe, and external dependencies (other cases, services, migrations).
4. **Riscos e rollback** — per risk: how to detect + how to mitigate; and how to revert (each committed task is a return point) — flag destructive or irreversible steps.
5. **Restrições para o agente** — execution guardrails: what **not** to touch, decisions already settled in the spec to respect, and where to **pause** for human review. Literal instructions — the agent follows them to the letter.
6. **Definição de pronto** — a **binary** checklist (each item MET/UNMET with `file:line` evidence, not "looks done"): every spec criterion has both an implementation and a test that proves it, tracked by number; full suite green; result.md written. It is the `cerne feature complete` gate.

**Ordering is deliberate, not cosmetic.** Estratégia and the Sequência de tarefas sit at the top and the Definição de pronto at the end, so the most decision-bearing, executable content and the exit gate land where long-context models attend best (primacy/recency). **Mark ambiguity, don't guess** — leave `[ESCLARECER: pergunta específica]` and resolve every marker before `cerne feature start`. The evidence behind this structure (context engineering / lost-in-the-middle, plan-and-solve decomposition, spec-driven plan→tasks, agentic-coding best practices) is registered in the backend as external references.

## Result structure (the template cerne scaffolds)

cerne scaffolds `result.md` from a canonical template when you **start** a feature (the same one `cerne result draft` asks the model to fill, with the spec and plan as context). The result **closes the loop**: the spec sets the acceptance criteria, the plan defines done, and the result **verifies done with evidence**. Record what shipped, not what was planned; **show evidence, never assertion**. **Right-size it** — a small delivery has short sections. Sections, **in order**:

1. **Resultado** — what shipped and the state (delivered / partial), in 1-2 sentences. The bottom line first.
2. **Aceitação** — each numbered spec criterion → **MET/UNMET (ATENDIDO/PENDENTE)** with **evidence** (test name, `file:line`, numbers). Binary and 1:1 with the spec — no "looks done".
3. **O que mudou** — the concrete changes from the reader's perspective: files/modules created and edited, new commands, observable behaviour.
4. **Débitos e follow-ups** — what's incomplete or deferred, as concrete work (a new feature/case), not an orphan TODO.
5. **Aprendizados e desvios** — what you learned and any intentional deviations from the spec/plan (what changed and why). Concise; "N/A" if nothing notable.

**Ordering is deliberate** (primacy/recency): Resultado and Aceitação lead — the outcome and its proof — and the softer learnings sit last. **Mark ambiguity, don't guess** — `[ESCLARECER: pergunta específica]`. The evidence behind this structure (Definition of Done, show-evidence-not-assertion, binary acceptance verification, Keep a Changelog) is registered in the backend as external references.

## Epics (large work, decomposed)

An **epic is just a feature that has children** — the role is inferred from the parent link, there is no separate entity, so the epic gets the full feature lifecycle/spec/plan/result for free. Use one when a body of work is too large for a single spec and naturally splits into sub-features.

- Create a child under an epic: `cerne feature new "<title>" --parent F-NNNN`.
- Link, move, or detach an existing feature later: `cerne feature reparent F-NNNN F-MMMM` (or `--clear`).
- **Epic ≠ module.** A module is the subsystem; an epic is decomposed work that may span modules. A feature can have both.
- **Read the breakdown, don't hand-maintain it:** `cerne status` shows an `ÉPICOS` section with `n/m` progress, and `.docs/CONTEXT.md` carries an *Épico* column plus a `## Épicos` section.
- The epic's lifecycle is independent (children completing does not auto-complete it). `cerne lint` flags inconsistencies — a live feature under a terminal epic, or an epic completed with open children.

## Decisions (ADRs)

Same model as a feature, with the **same two authoring flows as notes**:

- **One-shot (`--file`/stdin):** `cerne decision new "<title>" --file <adr.md>` (or piped stdin) is born `active` with the ADR already committed and pushed — the CLI confirms it (`✓ … já sincronizada`). Just `cerne pull` to materialize it locally; **no fill, no push**. Best when you already have the ADR text.
- **Stub (no body):** `cerne decision new "<title>"` mints `D-NNNN` (active) and scaffolds `content/decisions/D-NNNN.md` from the canonical ADR template (or run `cerne decision draft D-NNNN`); then `cerne pull`, fill the ADR in `.docs/current/decisions/D-NNNN-<slug>.md` (keep the `cerne_id` frontmatter), `cerne push`.

Supersede with `cerne decision substituir D-NNNN --by D-MMMM --reason "…"` — the status (active/superseded) is cerne data, not prose in the ADR.

## ADR structure (the template cerne scaffolds)

cerne scaffolds `content/decisions/D-NNNN.md` from a canonical ADR template when you create a decision (the same one `cerne decision draft` asks the model to fill). It follows Michael Nygard's classic spine (Context → Decision → Consequences), extended with the alternatives and code anchors that make a record durable. **Right-size it:** a small decision can leave the last sections as one line ("N/A") — don't inflate. Sections, **in order**:

1. **Contexto** — the forces and constraints in tension and the trigger (why decide now). Name the forces; ground them in real code/cases.
2. **Decisão** — the choice stated imperatively ("Vamos X" / "Usaremos X") with the two or three reasons that won.
3. **Consequências** — what follows: positives, **negatives/trade-offs (always list them)**, and constraints/invariants (what cannot change without revisiting).
4. **Alternativas consideradas** — the options weighed (2+ when there is a real trade-off), each with pros/cons and **why it was rejected**.
5. **Implementação e relações** — where the decision shows up in code (real files/modules) and the links: the features and ADRs it affects, supersedes, or that motivated it (use F-/D- ids).

**Ordering is deliberate** (primacy/recency): Contexto and Decisão lead; Implementação e relações is lookup material at the end. **The title records the decision, not the question** ("Use Postgres for primary storage", not "Which database?"). **Status is data, not prose** — the lifecycle is owned by cerne (`cerne decision substituir`), so the template has no Status section, and **an accepted ADR is a historical record: to change course, supersede it — don't rewrite it.** **Mark ambiguity, don't guess** — leave `[ESCLARECER: pergunta específica]` and resolve it before treating the decision as accepted. The evidence behind this structure (Nygard's ADRs, MADR, ThoughtWorks' lightweight ADRs, the create-adr skill) is registered in the backend as external references.

## Runbooks (operational procedures)

A **runbook** is an actionable operational procedure (deploy, smoke, recovery, migration, setup), separate from features and decisions. `cerne runbook new "<title>"` mints `RB-NNNN` (active), with the **same two authoring flows as notes**:

- **One-shot (`--file`/stdin):** `cerne runbook new "<title>" --file <runbook.md>` (or piped stdin) is born `active` with the procedure already committed and pushed; just `cerne pull` to materialize it locally — **no fill, no push**. Best when you already have the procedure written.
- **Stub (no body):** `cerne runbook new "<title>"` scaffolds `content/runbooks/RB-NNNN.md` from the canonical template (or run `cerne runbook draft RB-NNNN`); then `cerne pull`, fill it in `.docs/current/runbooks/RB-NNNN-<slug>.md` (keep the `cerne_id` frontmatter), `cerne push`.

Replace an obsolete one with `cerne runbook substituir RB-NNNN --by RB-MMMM --reason "…"` — the status (active/superseded) is cerne data, not prose. Runbooks surface in the `## Runbooks` section of `.docs/CONTEXT.md` (and `cerne search` / `cerne ask`). (Importing a hub `docs/runbooks/` tree registers each file as a runbook.)

## Runbook structure (the template cerne scaffolds)

cerne scaffolds `content/runbooks/RB-NNNN.md` from a canonical template when you create a runbook (the same one `cerne runbook draft` asks the model to fill). A runbook is read mid-incident, so every step is a command with its **expected output**, never a paragraph. **Right-size it** — a simple procedure has short sections. Sections, **in order**:

1. **Quando usar** — the trigger, symptoms or goal: the situation that fires this runbook, so the reader confirms in seconds it is the right one.
2. **Pré-requisitos** — access/tokens, tools and versions, environment state, window/approvals to confirm before starting.
3. **Procedimento** — numbered, action-first steps with copy-pasteable commands, each with its expected output ("você deve ver: X"); note idempotency where a step is safe to repeat.
4. **Verificação** — how to confirm success: observable checks/signals with expected output.
5. **Rollback e troubleshooting** — how to undo/recover (flag destructive steps), common failures as a symptom → cause → solution table, and **who to escalate to** when the runbook does not resolve.

**Ordering is deliberate** (primacy): the trigger, prerequisites and procedure lead — what an operator needs first under pressure. **Mark ambiguity, don't guess** — `[ESCLARECER: pergunta específica]`. The evidence behind this structure (Google SRE playbooks, PagerDuty incident response, lightweight runbook practice) is registered in the backend as external references.

## Notes (durable team knowledge)

A **note** is durable, team-relevant project knowledge an agent acquired across sessions — a gotcha, an invariant, a "why X and not Y", a tacit convention, a trap. It is **not** a feature (no work to gate), a decision (no formal choice), or a reference (no external URL): it is internal knowledge that today leaks into a per-user, per-machine scratchpad instead of reaching the guardian.

- Capture it: `cerne note new "<title>" [--informs F-NNNN,D-NNNN] [--file <body> | stdin]` mints `N-NNNN` (active). The **body is freeform** — there is no template and no `draft` command; write the knowledge and the *why*. `--informs` backlinks the note to the feature/decision (F-/D-) it informs.
- Author it — two flows, depending on whether you passed a body:
  - **Body on `new` (`--file`/stdin):** the note is born `active` with real content, already committed and pushed — the CLI confirms it (`✓ … já sincronizada`). Just `cerne pull` to materialize it locally. **No fill, no push** (a `cerne push` now would report nothing to send); don't pull-and-read just to check the body landed.
  - **Stub (no body):** `cerne pull`, fill `.docs/current/notes/N-NNNN-<slug>.md` (keep the `cerne_id` frontmatter), `cerne push`.
- Retire or correct it: **`cerne note retire N-NNNN --reason "…"`** retires an obsolete note (e.g. the feature it informed has shipped) — it moves to `superseded` with no successor, leaving `.docs/CONTEXT.md` but staying searchable history. To replace it with a corrected version instead, `cerne note substituir N-NNNN --by N-MMMM --reason "…"` (the successor materializes in its place). Either way the lifecycle (active/superseded) is cerne data, not prose.
- Let the guardian flag staleness: `cerne lint` reports a **`stale-note`** when an active note's `--informs` targets are *all* concluded (feature implemented/rejected/superseded, decision superseded). Two responses: **`cerne note keep N-NNNN`** marks it durable — it stays active and the lint stops flagging it — or `cerne note retire N-NNNN --reason "…"` retires it. cerne never auto-retires: the gotcha learned while building a feature is often the durable invariant worth keeping, so the call is yours.
- Find it: **`cerne note list`** lists the active notes (`--all` includes superseded history); **`cerne note show <id>`** prints one note's metadata + full body (or the generic **`cerne show N-NNNN`**, which reads any entity by id). Both read straight from the backend store, so they surface the knowledge **even when `.docs/` is stale** (locked snapshot not yet re-pulled) — prefer them over hunting through files. Active notes also appear in the `## Conhecimento` section of `.docs/CONTEXT.md` and in `cerne search` / `cerne ask`; superseded ones stay reachable through search/ask as history.

## Knowledge routing: cerne note vs. local memory

When you acquire knowledge worth keeping, route it by **who it belongs to**, not by reflex:

- **Durable, team-relevant project knowledge → `cerne note new`.** Gotchas, invariants, hard-won conventions, "why X and not Y" — anything a teammate or your own agent on another machine would need. It belongs in the guardian, where it is shared across machines and members, versioned, indexed, and governed (so it can be superseded instead of rotting). This is the gap notes exist to close: such knowledge otherwise dies in one person's local `~/.claude/.../memory/`.
- **Personal or machine-specific knowledge → stays in local Claude Code memory, never pushed.** Who you are and how you like to work (`user`/`feedback`-class) is personal — sharing it imposes one person's style on the team. **Machine-specific** facts are worse: a memory like "Go lives at `C:\…\go\bin`" is correct on one box and **wrong** on another, so pushing it is an actively harmful asset. Leave these where they are.
- **When in doubt, ask:** would this still be true and useful for a different person on a different machine? If yes, it is a note. If it only holds for *you* or *this box*, keep it local. Never bulk-sync memory into cerne — the boundary requires this per-item judgment.

## External sources (provenance)

When an external article / doc / spec informs a feature, decision, or fix, register it instead of leaving a bare URL in prose:

```
cerne reference new "<title>" --url <url> --informs F-NNNN,D-NNNN [--file <summary> | stdin]
```

It is marked external, its content is indexed (so `cerne ask` cites it), and `--informs` backlinks it to the entity it motivated.

## Hard rules

- **Register the task at the start.** The first action when you pick up a task is `cerne feature new "<title>"` (it lands as a candidate, visible on the kanban) — not something you do after the work is finished. Advance it as you go (spec → `approve`, plan → `start`, result → `complete`); if it is abandoned, `cerne feature reject <id> --reason "…"` so the board stays honest. Candidates left without a real spec past the grace window are flagged by `cerne lint` (`stale-candidate`).
- **Author through cerne**, not as loose markdown folders for tracked features/decisions — loose markdown is authoring input, not authority.
- **Never hand-edit generated outputs:** `.docs/CONTEXT.md` and the compiled wiki regenerate (`cerne pull` / `cerne compile`).
- After a `cerne push` that changes the backend, commit the updated `docs.lock` (the code↔docs pin) alongside your code change.
- `.docs/` is gitignored; commit `.cernerc`, `docs.lock` **and `.gitattributes`** in your project repo. The `.gitattributes` wires the `docs.lock` merge driver that `cerne init`/`cerne pull` register, so parallel branches/worktrees stop conflicting on the snapshot pin — a fresh clone just runs `cerne pull` once to self-heal its local git config.
