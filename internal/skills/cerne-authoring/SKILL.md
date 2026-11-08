---
name: cerne-authoring
description: "Use this whenever you create, plan, finish, or manage a feature, decision (ADR), runbook, note (durable team knowledge), or external reference in a project guarded by cerne. cerne is the documentation source of truth — author through it (cerne feature new -> pull -> push -> approve/start/complete), never as loose docs/case folders. Read current state from .docs/CONTEXT.md and `cerne status`. Route durable team knowledge an agent acquires to `cerne note new`, not the local Claude Code memory."
---

# Cerne-Native Authoring

## Overview

In a cerne-managed project the living documentation is owned by **cerne**: it holds the lifecycle of **features**, **decisions** (ADRs), **runbooks**, **notes** (durable team knowledge) and **references**, and is the source of truth for the current state. The backend (a git repo) is pinned by the committed `docs.lock`; `.cernerc` wires it. You author documents under `.docs/current/…` and push them back through cerne — you do **not** hand-create folders of loose markdown for tracked work.

This skill is the **mechanics and lifecycle**. The *content* of each document (the actual spec/plan/result prose) comes from your normal process. Read current state from `.docs/CONTEXT.md` and `cerne status` (or `cerne brief` for the cheapest whole-project map — every id + state + title, no paths; `--json` too — ideal to prime on the project, then `cerne show` only the ids you need); reach superseded/rejected history with `cerne search "<q>"` / `cerne ask "<q>"`. To open **one entity by its id** — any kind: feature, decision, reference, note, runbook, historical — run **`cerne show <id>`** (`--json` for machine output). It reads straight from the backend store, so it works even for implemented features that are not materialized in `.docs/` — use it instead of guessing a command like `cerne feature show`, which does not exist.

## When to use

- **The moment you pick up a task** — register it first with `cerne feature new "<title>" --context "<porquê>"` so in-flight work shows on the board from the start, not after it is done. Also for new decisions (ADRs) or registering an external source that informed your work.
- Moving a feature through its lifecycle (approve / start / complete).
- Any moment you would otherwise hand-create a tracked "case" or "feature" doc folder — don't; use this loop.

## Feature loop

cerne mints the canonical `F-NNNN` id and gates each transition on a real document.

**Register at the start, not the end.** The first action when you pick up a task is step 1 below — run it *before* you start building, so the work is visible on the kanban (as a candidate) from minute one instead of an agent running in the dark. The spec/plan/result you write then carry it rightward across the board; if the work is dropped, `reject` it. Creating the feature is cheap and self-limiting — you only mint one when real work actually starts, so the board never fills with speculative ideas nobody is working on.

1. **Create** — `cerne feature new "<title>" --context "<1-3 frases do porquê>" [--module <module>] [--facet <f>] [--component <c>] [--parent F-NNNN] [--fixes F-NNNN]` → candidate.
   - **Context is required at creation (F-0120):** a bare `feature new` is refused — a title-only candidate loses its creation context the moment the session that minted it is gone. The minimal path is `--context` (1-3 sentences of the *why*, seeded into the spec's Problema section). Already have the full spec? `--file <spec.md>` (or piped stdin) births the candidate with the real spec committed and pushed, one-shot like `note new` — no fill, no push. `--file`/stdin and `--context` are mutually exclusive.
   - `--module` assigns the subsystem when the project declares modules; omit for cross-cutting work.
   - **Tag it (two axes, like notes/decisions/references):** `--facet` and `--component` (both repeatable, comma-separated). **facet** is the closed, validated set — `infra`, `code`, `data`, `process`, `auth` (D-0002/D-0003); an unknown facet is rejected with **no** write. **component** is open vocabulary, slug-normalized (`Gitback` → `gitback`). Filter the board with `cerne feature list --facet <f> --component <c>` — AND across axes, and AND with `--state`/`--module`/`--parent`. Tags fold into the FTS body (so `cerne search`/`ask` find a feature by a tag absent from its prose) and render on the feature's line (and as chips in the kanban/feature page). Re-tag in place — any state except terminal history (superseded/rejected) — with `cerne feature retag F-NNNN --facet <f> --component <c>` (sets only the axis you pass; `--facet ""` clears it; never supersedes), so the implemented backlog can be categorized for retrieval too. Orthogonal to `--module` (product area) and `--parent` (epic decomposition).
   - `--parent F-NNNN` makes this feature decompose an epic. See **Epics**.
   - `--fixes F-NNNN` marks a correction of a delivered feature's defect (or `cerne feature set F-… --fixes F-NNNN` later; `""` clears). The process panel (`cerne stats --process`) counts it as an escaped defect when it is created within 28 days after F-NNNN's completion.
2. **Spec** — `cerne pull`, author `.docs/current/features/F-NNNN-<slug>/spec.md` (keep the `cerne_id` frontmatter), `cerne push`, then run `cerne feature approve F-NNNN --check` — it lists every impediment (spec still a stub, no valid `Barra:` line, open clarifications), writes nothing and is open to agents — then fix the hard impediments it lists; leave open clarifications for the human, with the approve request (they are the human's questions — never answer them yourself). Only then ask a human to run `cerne feature approve F-NNNN` (gate: a real spec, not the stub, naming its bar — and, by default, a human act; see **Human gates** below). `feature new --file` and `feature spec` end with the same readiness in one line.
3. **Plan** — `cerne feature approve` scaffolds `plan.md` from a canonical template (or run `cerne plan draft F-NNNN`); fill it in, `cerne push`, then `cerne feature start F-NNNN` (gate: a real plan, not the stub).
   - **An approved spec stays editable (F-0318).** Fix it the normal way — `cerne pull`, edit `.docs/current/features/F-NNNN-<slug>/spec.md` and `cerne push` (the feature page editor and `cerne feature spec` write the same document) — no revert. The approve keeps the spec it accepted; any spec whose content differs from it, even by a comma, stales the approval (frontmatter, line endings and trailing whitespace do not count): the state stays `approved`, but `cerne feature start` refuses ("spec changed since approval") until a human re-approves with `cerne feature approve F-NNNN` on the approved feature itself — a human gate like the first approve; the human sees only the delta on the feature page. `cerne show` prints `approval: stale` and `cerne next` lists it under WAITING FOR RE-APPROVAL, never READY TO START (`approval_stale` in both `--json`; queue `awaiting_reapproval` in `next --json`), so check before picking up an approved feature. Editing back to the approved text clears it. Once in development, spec edits never stale anything. A feature approved before the approved version was recorded is never stale; a human `cerne feature approve F-NNNN` on it records the current spec as its baseline.
4. **Implement** — build it per your project's stack and conventions; add tests; review.
5. **Result** — `cerne feature start` scaffolds `result.md` from a canonical template (or run `cerne result draft F-NNNN`); fill it as you finish, `cerne push`, then `cerne feature complete F-NNNN` (gate: a real result, not the stub). This lands the feature on `implemented` ("Ready" on the kanban) — code done, awaiting human validation. Right after `cerne feature complete F-NNNN`, run `cerne cost F-NNNN --stamp` (before the human accept): it writes the `## Custo` block from the session transcripts into the result, and the epic rollup reads it. No manual "Done" status, no index edits — `cerne feature accept` (see below) is the only way to reach the actual `done` state.

Terminal transitions when work is dropped or replaced: `cerne feature reject F-NNNN --reason "…"`; `cerne feature substituir F-NNNN --by F-MMMM --reason "…"`.

### Human gates: approve and accept (F-0131)

**Approval and acceptance are human gates by default.** Two points in the loop are reserved for a human unless the project's `.cernerc` explicitly opts out:

- **Start of the loop —** after writing the spec and a `cerne feature approve F-NNNN --check` with no hard impediment, **ask for approval and stop**, passing on any open clarification it lists. Do not run `cerne feature approve` yourself — an agent approving its own spec defeats the gate. Report the candidate is ready and wait.
- **End of the loop —** `cerne feature complete` lands the feature on `implemented` ("Ready" — code done, not yet validated), never `done`. Between the two, run `cerne cost F-NNNN --stamp` — after `cerne feature complete F-NNNN`, before the human accept — so the result records the feature's token cost (`## Custo`) for the epic rollup. `cerne feature accept F-NNNN` is the separate human-validation step that moves it to `done` (terminal, "Done" on the kanban). Do not run `cerne feature accept` yourself either — report the feature is implemented and ask a human to accept it.

Both gates read the project's `.cernerc` `[gates]` block (`approve`/`accept`, each `"human"` (default) or `"auto"`) together with the `CERNE_ACTOR` environment variable (`cerne hook install` stamps `CERNE_ACTOR=agent` into the project's `.claude/settings.json`, so an agent session is recognized). **Only run `cerne feature approve`/`accept` yourself when `[gates]` explicitly sets that gate to `"auto"`** for this project — check `.cernerc` if unsure; when in doubt, ask a human instead of guessing.

`cerne lint` flags a feature stuck on `implemented` past the project's stale-ready window (`stale-ready` rule) — that is your cue to ask a human to accept it, not to accept it yourself.

## Validate locally before pushing

Catch documentation debts **before** they churn the backend — same lint engine, no commit, no backend write:

- `cerne lint --staged` — runs every lint rule over your **local** `.docs/` edits (assembled over the pulled snapshot), without a pull. A partial `.docs/` (implemented features not materialized) yields no false positive.
- `cerne push --dry-run` — shows exactly which documents **would** go to the backend and lints them, then stops before any commit/push. Exits non-zero if they would lint dirty, so it doubles as a pre-push gate.

Both report the same findings (and the same `content/…` paths) as `cerne lint` over the backend — so a clean `--staged`/`--dry-run` means a clean push.

**Inspect the agent harness itself.** `cerne harness` (add `--json` for machine reading) reports the project's effective harness inventory, read-only: cerne-managed hooks and skills with drift against the running build (`divergent`/`missing` + the repair command, e.g. `cerne hook install`), MCP servers (secrets masked), marketplace plugins, context files and provider parity — each line with its origin layer, and items outside the repository flagged (another machine does not inherit them). Use it to answer "is this hook actually active here?" before debugging a hook that never fired.

**The model catalog (F-0311).** Every model list — the chat picker, its effort levels, the result stamp, the backend default — comes from one catalog: an embedded baseline plus a hand-editable global `models.json` in the user config dir, resolved field by field. `cerne ui`/`cerne serve` refresh it from the provider's listing at boot when it is older than 24h; `cerne models refresh [--force]` does it on demand and prints the diff (added / delisted / relisted). Demoting a model to legacy is a hand edit of the global file; never hardcode a model id in code (a guard test refuses it).

**Settings (F-0314).** cerne's configurable keys are declared in one registry; a value resolves project (`.cernerc`, committed) > host (`settings.toml` in the user config dir, never in the repo) > the declared default. `cerne settings list [--json]` shows each key's value, default and winning scope plus the file behind each scope; `cerne settings get <key> [--all] [--show-scope] [--show-origin]` prints one (`--all`: every scope's value, winner first; scope and `file:<path>`, as `git config` shows them); `cerne settings set <key> <value> --scope project|host` and `cerne settings unset <key> --scope project|host` edit only that key's line. Put a personal preference in the host scope so it never enters the team's diff.

**Acknowledging an intentional historical mention.** Sometimes an authored doc *should* name a superseded entity — a spec/ADR/note explaining "antes usávamos D-0003 (substituída por D-0005), e o trade-off era…". That is history, not a debt, but `cerne lint` flags it as `ref-to-superseded` by default. To declare the mention intentional, append the marker on the **same line**: `<!-- lint:ok-superseded D-0003 -->` (comma-separate several ids: `<!-- lint:ok-superseded D-0003, D-0005 -->`). The rule then tolerates that line only — an unmarked mention of the same id on another line is still a debt. Use it sparingly, for genuine historical context; it is an explicit acknowledgment, not a blanket silence.

## Syncing when the backend may have advanced (parallel agents)

Reconciliation is **per document**, so disjoint edits never collide:

- **`cerne sync`** = pull (merge) + push, retrying transient advances — the one forward command when another agent/worktree may have changed the backend. Disjoint documents reconcile automatically; a real **same-document** conflict stops non-destructively (your version kept in `.docs/`, the remote version stashed under `.docs/.conflicts/`) and names the file with a diff, so you resolve and `cerne push`.
- **`cerne push`** auto-rebases per document: a remote advance on a *different* document never blocks it; only the same document changed both ways is a conflict.
- **`cerne pull`** 3-way merges: it brings in the remote advance while keeping your unsent `.docs/` edits (it does **not** refuse on unsent edits). `--force` is only the explicit "discard my edits" escape — never the only way forward.

## Spec structure (the template cerne scaffolds)

cerne scaffolds a new feature's `spec.md` from a canonical template (the same one `cerne spec draft` asks the model to fill). Fill each section; the lifecycle gates treat the empty scaffold as a stub until you do. When the design benefits from a sequence diagram, `cerne spec diagram F-NNNN [--name <name>]` AI-drafts a Mermaid diagram artifact for it (delegation prompt, or generated directly in byo-key/claude-cli `[llm]` mode). Sections, **in order**:

1. **Problema** — what is missing or broken **today**, grounded in real code (`file.go:line`); why now; the cost of not fixing it. Problem only — no solution yet.
2. **Objetivo e critérios de aceitação** — one objective sentence, then **numbered, testable** criteria (1 criterion : 1 test). For behaviour, use EARS — *"QUANDO `<condição>` O SISTEMA DEVE `<comportamento>`"*; structural invariants as a direct, measurable assertion.
3. **Decisões do design** — a `| Fork | Decisão | Razão |` table; record the rejected alternatives, not just the chosen path.
4. **Desenho** — by module/package, grounded in real files, with explicit contracts (signatures, schemas, endpoints, events). Include the *how* — don't strip it out.
5. **Verificação** — the test plan (TDD), 1:1 with the acceptance criteria.
6. **Fora de escopo** — what you will **not** do now, and why (or what becomes a follow-up).

**Ordering is deliberate, not cosmetic.** Problema and the acceptance criteria sit at the top and Verificação near the end, so the most decision-bearing, testable content lands where long-context models attend best (primacy/recency) rather than buried in the middle. Keep it dense — a bloated spec hurts more than it helps.

**Mark ambiguity, don't guess.** Wherever something is unresolved, leave `[ESCLARECER: pergunta específica]` instead of a plausible-but-maybe-wrong guess, and resolve every marker before `cerne feature approve`. **Write the marker in the doc's own language** — the vocabulary is a per-language catalog (pt `[ESCLARECER:]`/`[ESCLARECIDO:]`, en `[CLARIFY:]`/`[CLARIFIED:]`) and cerne recognizes all of it everywhere (UI chips, tab badge, lint, approve gate), so an English doc uses `[CLARIFY: specific question]` and gates exactly like a pt-BR one. Options work the same in any vocabulary (`[CLARIFY: where? | doc | sidecar]`), and answering via the UI writes the resolved form in the marker's own vocabulary.

The evidence behind this structure (lost-in-the-middle / primacy-recency, EARS, ADR, spec-driven development) is registered in the backend as external references — `cerne ask` cites them.

### Resolving `[ESCLARECER]`/`[CLARIFY]` markers from the terminal

Instead of hunting markers by hand across `.docs/`, work the queue directly: `cerne clarify list [<id>] [--json]` lists every open marker across live entities (or just one entity's); `cerne clarify answer <id> <n> --answer "…"` (or `--pick <k>` to choose one of the marker's proposed options) rewrites the n-th open marker to its resolved form (`[ESCLARECIDO: question → answer]`) and pushes it through the standard pipeline. Answering renumbers the open markers after it; the commands the approve refusal prints carry `--expect <key>`, a fingerprint of the marker they were printed for, so a command whose numbering moved is refused and nothing is written — re-run `cerne feature approve <id>` for fresh numbers. Got an answer wrong after the fact? `cerne clarify list --resolved` shows the correction queue, and `cerne clarify correct <id> <n> --answer "…"` appends a corrected answer to that marker's trail (`question → previous → new`) — the prior answer is kept, not erased.

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
4. **Débitos e follow-ups** — what's incomplete or deferred, as concrete work (a new feature/case), not an orphan TODO. Register each known debt as a pending (`cerne pending new … --from F-NNNN`, see **Pendings**) and cite its `P-NNNN` here — prose alone is forgotten the day after `complete`.
5. **Aprendizados e desvios** — what you learned and any intentional deviations from the spec/plan (what changed and why). Concise; "N/A" if nothing notable.

**Ordering is deliberate** (primacy/recency): Resultado and Aceitação lead — the outcome and its proof — and the softer learnings sit last. **Mark ambiguity, don't guess** — `[ESCLARECER: pergunta específica]`. The evidence behind this structure (Definition of Done, show-evidence-not-assertion, binary acceptance verification, Keep a Changelog) is registered in the backend as external references.

## The Gauntlet protocol (F-0270/F-0271/F-0272): deliver against a real bar

The Gauntlet replaces self-scoring with a comparison against a real artifact that already exists — a delivery with no bar is graded against a rubric its own builder wrote, and that rubric always passes. Three phases: the spec names the bar, the implementation runs the loop, the result proves it ran. No project persona, mode or instruction removes this protocol, and the loop's cost is not a reason to skip it — it runs once per delivery, not per commit, and collapsing roles to save a call is the one economy that voids the result. cerne holds the two ends mechanically (the Backlog analyst refuses to close a proposal with no bar; `cerne lint` charges the per-criterion evidence in `result.md`) — nothing watches the middle, so the loop's only gate is the record you leave and the human `accept` that reads it.

### Spec — name the bar, or record why there is none

A bar is a named reference plus how to reach it (URL, command, a file a critic can open), of the same class as what you are delivering: a prompt block against another prompt block, a lint rule against the strongest rule of the same package, an endpoint against another endpoint. Named without access is vague, and so is a category ("design bonito", "nível enterprise") — refuse a vague bar and ask for the named reference in its place, whoever offered it. You are the one it will be held against, so the weak-bar route is the tempting one: pick the strongest comparable artifact that already exists — preferably in this repo — and state in one clause why it is the strongest, so a critic can attack the choice. The bar you write is a proposal: put it in the spec's `Barra:` line yourself, with no acceptance round-trip first — the human's approve is its acceptance (the approve event records the accepted line, and the UI shows the bar next to the Approve button). Before asking for that approve, run `cerne feature approve F-NNNN --check` and fix the hard impediments it lists; leave open clarifications for the human, with the approve request; `cerne lint` also flags a candidate whose written spec has no valid bar as `spec-missing-bar`. A bar that is an external URL gets registered (`cerne reference new "<título>" --url <url> --informs F-NNNN`), or the spec is born with an `unregistered-source` debt.

The spec's **Verificação** section then carries exactly one of three literal forms — pt-BR, because they are written inside the spec:

```
Barra: <referência nomeada + acesso>
Método: comparação cega, lado a lado, por crítico de contexto fresco que não participou da construção
Saída: vencer ou empatar; derrota abre nova rodada; parar perdendo só por encerramento humano registrado
```

Filled in, the whole `<…>` gives way to the value — `Barra: modo spec do Kiro — kiro.dev/docs/specs; mesma classe: conduzir humano até spec completa`; a line that keeps the `<…>` is refused by the approve.

— or the single line `Barra: não aplicável (<justificativa de 1 linha>) — padrão alternativo: <teste/benchmark/invariante>`, when the delivery has no comparable artifact (a point fix, pure backend with nothing observable): that classification is a proposal too, made in the open — the justification in the line, never a silent omission — and the human's approve confirms it; a human who disagrees does not approve. — or the single line `Barra: dispensada por decisão humana (<citação curta da fala que dispensou>)`, which exists only when a human refuses the bar on their own; you never suggest it, and the quoted words are what separate it from forgetting. A silent absence of a bar is never acceptable; a recorded exemption is.

### Implementation — builders, then a critic, then a judge

Three roles, three separate agents. Keeping them separate *is* the protocol — one agent playing two collapses it back into self-scoring, and "I will reread my own diff with fresh eyes" is that collapse:

- **builders** implement the plan TDD (red test → minimal green → commit), one concern per commit.
- **A fresh-context critic** attacks the finished diff. It is never the builder and never inherits the build conversation: whoever wrote the thing rationalizes it, and whoever watched it being written adopts its framing. Spawn it as a separate agent, hand it one file path (the diff or artifact written to disk) under the rule "read exactly this file, do not run git, do not read the repository", and delimit the artifact explicitly in the prompt (`=== DIFF (início) === … === DIFF (fim) ===`) — a fresh prompt inside your own session is not fresh context, and without the fence your own instructions are read as part of what is being judged. Demand the exact excerpt behind each finding; one without an excerpt is unverifiable. Capture the artifact the way its consumer receives it — the rendered runtime value, the served page, the real command output — never as a cut from the source when an assembly layer (concatenation, template, escape) stands between the two: that layer resolves away in the product but survives in the excerpt, handing the critic defects the consumer never sees.
- **A blind judge** compares your artifact against the bar. Write the two to neutrally named files, vary which position yours takes, and strip what gives it away (feature ids, paths, headings naming the origin). The bar comes out by the same consumer path, never from the source. When a finding from judge or critic is about form — rendering, escape, truncation, assembly residue — verify it against the runtime before it becomes a correction, a debt or a feature: an extraction artifact yields exactly this kind of plausible, specific finding. Ask one question — the job, named ("which text instructs an implementing agent better", "which rule is more robust") — and require the verdict as a fixed last line: `VEREDITO: A`, `VEREDITO: B` or `VEREDITO: EMPATE`. A verdict you have to interpret is one you will interpret in your own favour; any answer without that line counts as a defeat.

Every critic finding gets an explicit disposition, on the record: **corrected**, **refuted with evidence** (the existing code already handles it — name the `file:line`), or **accepted as a documented limit** (a declared debt or a follow-up feature). "Considered" is not a disposition; a finding that quietly disappears is a defect you chose to ship.

The judge decides the exit — **win or tie** — and you use a new judge every round: one that already ruled on your artifact defends its verdict instead of rereading it. A defeat produces corrections and another round; never a fixed number of rounds, and never a quiet swap for an easier bar (changing the bar is a spec change, not a round). The only way to stop while losing is an explicit human decision, recorded. Expect the judge to find real holes in the bar itself — route those as a separate improvement (a new feature, or a note), never fix the bar inside the delivery it is judging.

### Closing — the result carries the proof

`result.md` maps each numbered spec criterion to its evidence and must cite the number in words — `critério N` — because that literal citation is what `cerne lint` looks for: the `result-missing-evidence` rule flags an implemented feature whose result never mentions a criterion the spec numbered. Evidence is a test name, a `file:line`, a number; "atendido" on its own is an assertion.

Then record the loop under its own heading, one line per round:

```
## Gauntlet
Barra: <referência + acesso>
Rodada <n> — <vitória|derrota|empate>: "<citação curta do veredito do juiz>"
Achados do crítico: <n> — corrigidos <…>; refutados <… com file:line>; aceitos como limite <…>
```

The quoted verdict is what makes the record checkable at the human `accept` gate: a Gauntlet section with no quoted verdict per round and no disposition per finding cannot be told apart from a loop that never ran.

`cerne feature complete` itself refuses a result whose record the lint would flag (a numbered criterion never cited, or no valid `## Gauntlet`), and `--force` does not cross it. Fix the result while the doc is still editable: once the feature is `implemented` the result is frozen out of `.docs/` — until the `accept` it changes only through `cerne feature amend` (see **Errata** below).

A critic-only pass (no judge) goes on the `Achados do crítico` line, never on a `Rodada N` line — a `Rodada N` line must carry the judge's quoted verdict.

## Epics (large work, decomposed)

An **epic is just a feature that has children** — the role is inferred from the parent link, there is no separate entity, so the epic gets the full feature lifecycle/spec/plan/result for free. Use one when a body of work is too large for a single spec and naturally splits into sub-features.

- Create a child under an epic: `cerne feature new "<title>" --context "<porquê>" --parent F-NNNN`.
- Link, move, or detach an existing feature later: `cerne feature reparent F-NNNN F-MMMM` (or `--clear`).
- **Epic ≠ module.** A module is the subsystem; an epic is decomposed work that may span modules. A feature can have both.
- **Read the breakdown, don't hand-maintain it:** `cerne status` shows an `ÉPICOS` section with `n/m` progress, and `.docs/CONTEXT.md` carries an *Épico* column plus a `## Épicos` section.
- The epic's lifecycle is independent (children completing does not auto-complete it). `cerne lint` flags inconsistencies — a live feature under a terminal epic, an epic completed with open children, or an epic still candidate/approved while a child is already in development (`epic-behind-children`: work started but the epic was never planned/started — write its plan and `cerne feature start` it).
- **Declaring intent upfront:** when a PRD is known from the start to decompose, `cerne epic new "<title>" --context "…"` mints a **declared** epic (`E-` prefix) instead of `feature new` — same lifecycle/gates, the prefix just signals the intent at birth (epic *behavior* is always structural: any feature with children). Decompose it with `cerne task new "<step>" --parent E-NNNN --context "…"` — a **task** (`T-` prefix) is a PRD execution unit already past its human gate: it is born `approved` (`cerne feature start T-NNNN` needs no plan of its own — the why lives in the parent PRD), but `cerne feature complete T-NNNN` still requires a real `result.md`. `--parent` on a task must name a committed feature/epic (approved or beyond).

**Housekeeping on a feature/epic in place (no lifecycle change):**

- `cerne feature rename F-NNNN "<new title>"` — allowed until development starts; frozen once implemented/terminal.
- `cerne feature spec F-NNNN --file <spec.md>` (or piped stdin) — replaces the whole spec of a **candidate** or an **approved** feature, same gated write the Backlog chat performs on confirmation; no state change, no retitle. On an approved feature a different spec stales the approval (F-0318, see **Plan** above). Refused past `approved`.
- `cerne feature set F-NNNN --owner <name> --priority p1` — sets owner and/or priority in place (only the flag passed changes; empty string clears); doesn't touch state, module, tags, or docs.
- `cerne feature revert F-NNNN --reason "…"` — undoes an approval, back to `candidate` (spec/plan stay intact).
- `cerne feature deliver F-NNNN --commit <sha-or-url> --mr <url>` — records the delivery commit/MR post-hoc, after the MR merges.
- `cerne feature capability F-NNNN C-0001,C-0002` — links the feature to nodes in the product-capability taxonomy (`--clear` unlinks). Manage the taxonomy itself with `cerne capability new "<name>" [--parent C-NNNN]`, browse it with `cerne capability list --tree`, retire a node with `cerne capability retire C-NNNN --reason "…"` (refuses with active children or linked features), or propose the whole tree from existing specs with `cerne capability bootstrap [--apply]`.

## Decisions (ADRs)

Same model as a feature, with the **same two authoring flows as notes**:

- **One-shot (`--file`/stdin):** `cerne decision new "<title>" --file <adr.md>` (or piped stdin) is born `active` with the ADR already committed and pushed — the CLI confirms it (`✓ … já sincronizada`). Just `cerne pull` to materialize it locally; **no fill, no push**. Best when you already have the ADR text.
- **Stub (no body):** `cerne decision new "<title>"` mints `D-NNNN` (active) and scaffolds `content/decisions/D-NNNN.md` from the canonical ADR template (or run `cerne decision draft D-NNNN`); then `cerne pull`, fill the ADR in `.docs/current/decisions/D-NNNN-<slug>.md` (keep the `cerne_id` frontmatter), `cerne push`.

Supersede with `cerne decision substituir D-NNNN --by D-MMMM --reason "…"` — the status (active/superseded) is cerne data, not prose in the ADR.

Tag it (two axes, **like notes**): `cerne decision new "<title>" --facet <f> --component <c>` (both repeatable, comma-separated). **facet** is the closed, validated set — `infra`, `code`, `data`, `process`, `auth` (D-0002); an unknown facet is rejected with **no** write. **component** is open vocabulary, slug-normalized (`Keycloak` → `keycloak`). List and filter them with `cerne decision list [--facet <f>] [--component <c>] [--all] [--json]` — the **only** listing for decisions (AND across axes; active by default, `--all` includes superseded; `--json` for machine reads). Tags fold into the FTS body (so `cerne search`/`ask` find a decision by a tag absent from the ADR prose) and render on the decision's line in `.docs/CONTEXT.md`. Re-tag an active decision in place with `cerne decision retag D-NNNN --facet <f> --component <c>` — it sets only the axis you pass (`--facet ""` clears that axis) and never supersedes.

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

Replace an obsolete one with `cerne runbook substituir RB-NNNN --by RB-MMMM --reason "…"`; when the procedure is simply gone — the flow was discontinued, or it turned into an explanation that belongs in a note — retire it instead with **`cerne runbook retire RB-NNNN --reason "…"`** (active → superseded with **no** successor: it leaves `.docs/CONTEXT.md` but stays searchable history). Either way the status (active/superseded) is cerne data, not prose. Runbooks surface in the `## Runbooks` section of `.docs/CONTEXT.md` (and `cerne search` / `cerne ask`). (Importing a hub `docs/runbooks/` tree registers each file as a runbook.)

Tag it (two axes, like notes/decisions) — but note the difference: **`cerne runbook new` takes no tag flags**; set tags in place with `cerne runbook retag RB-NNNN --facet <f> --component <c>` right after creating it. `retag` alters only the axis you pass (`--facet ""` clears that axis) and never supersedes; **facet** is the closed `infra`/`code`/`data`/`process`/`auth` set, **component** is open vocabulary, slug-normalized. Tags fold into the FTS body (so `cerne search`/`ask` find a runbook by a tag absent from its prose) and render as chips in the UI; a live runbook untagged on both axes is flagged by `cerne lint` as `untagged-live`.

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
- Tag it (two axes, set at `note new`): `cerne note new "<title>" --facet <f> --component <c>` (both repeatable). **facet** is a closed, validated set — `infra`, `code`, `data`, `process`, `auth` (D-0002); an unknown facet is rejected with **no** write. **component** is open vocabulary, slug-normalized (`Gitea` → `gitea`). Filter the roster by them: `cerne note list --facet infra --component gitea` (AND across axes). Tags render inline in `note list`/`note show`, fold into the FTS body (so `cerne search`/`ask` find a note by a tag absent from its prose), and show in the `## Conhecimento` section of `.docs/CONTEXT.md`. Re-tag an active note in place with `cerne note retag N-NNNN --facet <f> --component <c>` — it sets only the axis you pass (`--facet ""` clears that axis) and never creates a successor. Leaving a live entity untagged on **both** axes is itself a documentation debt: `cerne lint` flags an active note/decision, a current reference, or a **live feature** (candidate/approved/in_development — the implemented backlog is spared) with no facet **and** no component as **`untagged-live`** — an untagged entity is invisible to the `--facet`/`--component` filters and reachable only by full-text search. It is advisory (it never blocks creation), so tag at `new` or clear it later with `retag`.

### Conventions: prescriptive notes an agent pulls before building (F-0165)

A note whose components include the reserved slug **`convention`** is a **convention**: prescriptive guidance (*how to build here* — form validation rules, money/date fields, menu assembly, layout standards), as opposed to a descriptive note (*what was discovered*). `cerne note list` marks it `convention`. Author one like any note, pairing `convention` with the topic component: `cerne note new "Formulários: validação e campos" --component convention,forms --file <body>`. When an agent gets something wrong, don't fix it only in chat — **supersede the convention with a better one** (`cerne note substituir N-NNNN --by N-MMMM --reason "…"`): the correction becomes the standard the next agent reads.

**Before building a page, form, or feature slice: consult the conventions.** Run **`cerne note tags`** (`--json` for machines) — it prints the tag catalog across active notes (each facet/component with counts), i.e. the topic menu — then pull only what's relevant: `cerne note list --component <topico>` and `cerne note show N-NNNN`. The plan template prompts this lookup in Estratégia and asks you to **cite the applicable N-NNNN ids in the plan**, so the conventions that govern the work are on the record before coding starts.

## Agent activity registry (work-in-progress across agents/machines)

Not every task is a feature. Investigating an error, reviewing an MR, doing an ops chore — these are transient activity, not a lifecycle to gate, yet another agent (this machine, another, or a teammate) has no way to see they're already in progress without it. `cerne work` closes that gap with a lightweight registry in the same shared backend:

- **The moment you pick up a non-feature task**, register it: `cerne work start "<description>" [--ref F-NNNN,D-NNNN] [--agent <name>]` mints `W-NNNN` (active). Actor defaults to `<user>@<hostname>`; `--ref` links it to the feature/decision it relates to (unknown id aborts, nothing written).
- **Before delegating an error investigation to another agent**, run `cerne work list` first — if someone (or some other agent) is already on it, don't spin up a duplicate. `--all` also shows recently terminated activity; `--json` for machine reads.
- **When you finish**, close it: `cerne work done W-NNNN [--note "<outcome>"]` (succeeded) or `cerne work abandon W-NNNN --reason "<why>"` (gave up/superseded). Both are terminal — the record stays as queryable history, never deleted.
- Active work also surfaces in the `## In progress` section of `.docs/CONTEXT.md` (omitted when nothing is active) — visible to any agent reading `.docs/` without running the CLI — and `cerne lint` flags an active work stuck past the project's `stale_work_hours` window (default 8h, `stale-work` rule) as an agent that likely died without closing it.
- **Feature implementations register themselves (F-0146):** `cerne feature start` auto-opens a work (actor + ref, same commit) and `complete`/`supersede` auto-closes it — do NOT add a manual `work start` for a feature you are implementing; the manual flow is only for non-feature tasks. Per-project opt-out: `[activity] auto = false` in `.cernerc`.
- **The duplicate guard is mechanical (F-0147):** `feature new` and `work start` refuse a title similar to a live feature or an active work, listing the matches — with multiple agents, the same bug gets registered twice, and this is the backstop. If a match appears, join the existing registration instead of minting another; `--force` is the conscious override for a genuinely distinct problem that reads alike (the override lands on the creation event as `guard: forced`). The same guard covers `note new`, `decision new` and `reference new` (F-0324): they refuse a similar **live** entity of the same type — notes and decisions by title, references by normalized URL (a similar title with a different URL never blocks) — naming the existing id with next steps that run as printed (`cerne show`, `note retire`/`reference retire`, or for a decision, `--force` then `substituir`); `--force` records `guard: forced` on `note.created`/`decision.created`/`reference.created`. `cerne lint` reports a **`duplicate-live`** finding per group of live same-type duplicates, with a runnable remedy; a group with a `guard: forced` member is acknowledged and silent.

## Pendings (delivery debts, F-0322)

A pending (`P-NNNN`) is a **known debt of a concrete delivery** — "case Y of F-0231 was left out" — with an address, a lifecycle and a place where it reappears. It is not an idea: the origin is mandatory, which is what keeps it apart from speculative backlog.

- **Register pendings when you close a feature:** for every debt you would write in the result's *Débitos e follow-ups*, run `cerne pending new "<title>" --from F-NNNN [--file <context.md> | stdin] [--facet <f>] [--component <c>]` (also `T-`/`E-` origins) and cite the `P-NNNN` in the result. `--from` is required and must exist — a missing or unknown origin is refused with nothing written. Without `--facet`/`--component` the pending inherits the origin's tags, per axis.
- **Duplicate guard:** an **open** pending of the **same origin** with a similar title refuses the creation, naming the existing id — a retry never mints a second debt (`--force` for a genuinely distinct one, recorded as `guard: forced`).
- **Read:** `cerne pending list` (open only by default; one line per pending: id, age in days, origin, title; `--state open|resolved|dropped|promoted|all` (with `all` each line also shows its state), `--from <id>`, `--component <c>`, `--module <m>` of the origin, `--json`). `cerne pending show P-NNNN` (or `cerne show P-NNNN`, `--json` on both) prints one; `cerne show F-NNNN` lists the open pendings the delivery left.
- **Close (not human gates — whoever paid the debt closes it):** `cerne pending resolve P-NNNN` (paid), `cerne pending drop P-NNNN --reason "<why>"` (discarded; the reason is required), `cerne pending promote P-NNNN --as feature|task [--module <m>] [--force]` (creates a candidate feature, or a task under the origin — which must be approved or in development, never implemented/done; for a closed origin use `--as feature` — inheriting title, tags, the origin's module (`--module` overrides it) and context; the generated spec links back with `**Origem:** pendência [[P-NNNN]] (de F-NNNN)` and the pending records `promoted_to`; the created entity passes the F-0147 guard, and `--force` overrides it when the debt merely reads like its origin). All three are terminal.
- **Where they reappear:** open pendings get their own block in `cerne next` (oldest first, capped), and `cerne lint` reports an open pending older than 30 days as **`stale-pending`** — an advisory **warning** that never fails `cerne lint` on its own, attributed to the origin so `cerne lint --feature/--parent` on that delivery shows it.
- **A pending never blocks the lifecycle:** approve, start, complete and accept run as if it did not exist. It is a reminder, not a gate.

## Ideas (raw material, F-0312)

An idea (`I-NNNN`) is **not a demand**: a candidate is a demand we already decided is worth evaluating; an idea is raw material nobody has evaluated yet ("maybe one day X"). Capturing one costs nothing, and it stays outside the funnel by design — it never appears in `cerne lint`, `next`, `status`, `stats`, `brief`, the compiled wiki, the graph, the board/kanban, the Backlog's candidates or the activity heatmap. Don't register raw material as a `candidate` (that drags the full spec + tags package and pollutes the funnel), and don't leave it as a `// TODO`: capture it as an idea.

- **Capture:** `cerne idea new "<text>" [--facet <f>] [--component <c>]` mints `I-NNNN` (active) with nothing else required — no spec, module, work or tags.
- **Read:** `cerne idea list` (active only, newest first: id, date, state, first line; `--all` adds dropped and promoted ideas with the reason or the feature; `--search <term>` keeps ideas whose text contains every word of the term, in any order, case- and accent-insensitive; `--json`). `cerne idea show I-NNNN` (or `cerne show I-NNNN`, `--json` on both) prints one.
- **Drop:** `cerne idea drop I-NNNN [--reason "<why>"]` — active -> dropped; the record is kept and shows in `--all` with the reason. Terminal.
- **Promote:** `cerne idea promote I-NNNN [--title "<t>"] [--module <m>] [--force]` — creates a **candidate** feature titled by the idea's first line, inheriting its tags, with the text as the spec's context under `**Origem:** ideia [[I-NNNN]]`; the idea records `promoted_to` and leaves the active list. Prints both ids. A dropped or promoted idea is refused, naming its state and feature; an idea whose first line is exactly a live feature's title needs `--title` (and `--force` if it still reads alike). From there the candidate is a regular demand: complete its spec and tags in the same act (the candidate rules apply after the promotion, never to the idea), and fix a poor derived title with `cerne feature rename`.
- **On demand only (agents):** ideas are never in the automatic context — not in the chat's priming, the roster or retrieved search results — so never pull ideas in unasked. Only when the user asks for ideas on a subject, run `cerne idea list --search "<term>"` and cite the `I-` ids you used.

## Permanent channel (F-0325): quick coordination, no ceremony

Every project has one always-on channel for short exchanges between agents with open work and the human ("touching `internal/server/rooms.go`, hold on"; "contract done, consume it"). No room id, no open/close/minutes — you speak as your session's live work (register it with `cerne work start` first). Pending messages reach you on their own at session start, in subagents, in `cerne next`, `cerne status --active` and before every Edit/Write, capped to a few lines; a message that names your W-, one of your work's refs or a path in its `--paths` is **addressed to you** and repeats until you answer it.

    cerne msg send "<text>"     # post as your work; says which works with a live session will get it
    cerne msg read [--json]     # the whole live channel, each message's delivery state (not_delivered / delivered / answered)
    cerne msg ack <seq>...      # silence an addressed message without replying (any later message of yours also counts)

Messages expire after 24h, or when the sender's work closes. No read receipts: the delivery state is derived, never typed.

## War room (F-0211/F-0226/F-0227/F-0228): live multi-agent conversation

A **war room** is a live, ephemeral chat channel served by a running `cerne ui` — for real-time back-and-forth between agents/humans on a shared problem, distinct from the async `cerne work` registry above. **Opening a room is a human moderator's act (F-0211/F-0217).** A human opens one with `cerne room open "<topic>"` — you do **not** run this yourself; you join a room when invited, and only ever run `cerne room open` on an explicit human request.

**The cycle (F-0226: a joined session capability, not `--nick`, is the authority for everything you say):**

```
cerne room list                                                    # discover open rooms
cerne room join <id> --nick <apelido> [--bio "..."]                 # mints a session capability, once per room — keep it
export CERNE_ROOM_SESSION=<printed by join>                         # or pass --session on every call below
cerne room say <id> "<msg>" --session <token>                        # speak, authenticated by the session's nick
cerne room watch <id> --session <token> [--once]                     # F-0227: turn-based wakeup with a durable cursor, filters your own echo
cerne room read <id> --since <seq> --once                            # F-0220 primitive: stateless wakeup, no cursor/session/echo-filter
cerne room close <id> --facilitator <token> --minutes <...>          # F-0228: facilitator-only, structured minutes — see below
```

**`room watch --once`, not `room read`, for a turn-based agent (F-0227).** `watch` wraps the same long-poll primitive as `read` with what a turn-based agent actually needs: a durable cursor (a restart resumes where you left off — no manual `--since` bookkeeping), and it filters out your **own** messages so `say`-ing something doesn't consume your own wakeup. `--once` long-polls until the first **relevant** (non-echo) event, prints it, and exits 0; a room `close` event always ends the loop, printed, regardless of `--once`. `--session`/`CERNE_ROOM_SESSION` is required (same convention as `say`); the cursor is a local file, not a `--since` you track by hand. `watch` never blocks forever on its own the way `--follow` would — retry a transient connection failure with its own bounded backoff, or re-run `--once` for the next turn.

`room read --once`/`--follow` is still there underneath as the stateless primitive (no session required, no cursor persisted) — reach for it in a script or a one-off check where you don't want a cursor file on disk, not as the default turn-based wakeup.

**Facilitation and structured close (F-0228).** `cerne room open` also mints a **facilitator capability**, printed once and separate from any participant session — it is the authority to triage and close the room, never appears in the roster/transcript/list, and rotates atomically via `cerne room facilitator handoff <id> --facilitator <current>` (prints a new one, the old one stops working immediately — the way an opener agent hands the role to a human over an external channel). Any joined participant can raise something the facilitator needs to see:

```
cerne room propose <id> --session <token> --title "<t>" --body "<why>"           # a proposal
cerne room request-decision <id> --session <token> --title "<t>" --body "<why>"  # a decision request
cerne room pending <id> --facilitator <token>                                     # facilitator's triage queue (no LLM synthesis)
cerne room resolve <id> <item-id> --facilitator <token> --result "<outcome>"       # facilitator-only
```

Closing requires the facilitator capability and a structured **minutes** JSON (`conclusion`/`rationale` required, `discarded_options`/`owners`/`next_steps` may be empty lists) — `cerne room minutes template` prints a blank scaffold to fill in. By default close **refuses** while facilitation items are still open and lists them; `--allow-open-items` forces it through and records the exception in the minutes. `cerne room minutes show <id> --format markdown` renders a closed room's minutes deterministically (no LLM) for copying elsewhere — it never creates a feature/decision/note or commits anything on its own.

**Registering the minutes as a governed note (F-0238).** A closed room's ata lives only in the room's transcript — invisible to `search`/`ask`, with no backlinks — until someone explicitly bridges it: `cerne room minutes register <id> [--informs F-…,D-…] [--facet f] [--component c]`. This is the **only** path that creates anything durable from a room; it is never automatic on close. It reads the closed room's minutes via the server, creates a **note** (N-) whose body is the same deterministic projection as `minutes show` plus provenance (objective, start/end, participants), always tagged with the reserved component `ata` (filter with `cerne note list --component ata`), and records the link on the room (shown by the UI as "Ata registrada: N-NNNN"). It refuses an open room (close it first) and refuses to re-register an already-linked room unless `--force` (e.g. an errata). After registering, cite the note's id-form wiki-link (`[[N-NNNN]]`) in the Origem line of any spec/decision the meeting produced — that backlinks the graph from room → minutes → derived entities.

**Troubleshooting a failed `cerne room join`, self-diagnosable without a human:**
- **Connection error ("no cerne ui answering ... start one with 'cerne ui'")** → no `cerne ui` is listening on the port (default 7717). Start one: `cerne ui` in the project directory.
- **"no cerne ui running this project" (404)** → a `cerne ui` is up but not serving *this* project directory. Not yours to fix — tell the human to restart `cerne ui` in the correct project directory.
- **Conflict, nick already taken (409)** → someone else in the room already holds that `--nick`. Pick a different one and retry.

## Errata (correcting a concluded, immutable entity)

A concluded entity — a `done`/`rejected`/`superseded` feature, a `superseded`/`obsolete` decision, runbook or note — is **history: its body is immutable**. When such a record carries a stale reference or needs a clarification, do **not** rewrite it and do **not** reopen it. Append an **errata** instead.

**One window before that (F-0305): the result of an `implemented` feature, before the human `accept`.** `implemented` is "awaiting validation" and the FSM has no way back, so a result found wrong in validation is corrected in place, with a reason:

```
cerne show F-0008 --doc result > result.md    # edit it
cerne feature amend F-0008 --file result.md --reason "<why>"
```

The amend replaces only `result.md` (never spec or plan), records the reason on a `feature.doc_edited` event, and is refused when the new result fails the result checks (evidence per criterion, Gauntlet record). Any other state refuses it and names its path: `.docs/` + `cerne push` while live, errata from `accept` on. A changed document of a feature past the live states is refused by `cerne push` and by the feature page alike — once the feature leaves development, `.docs/` edits no longer reach it: carry the edit through the state's path (the push refusal points `amend --file` at the local copy itself), then delete the local copy, or every later push stays refused. For everything else, errata:

```
cerne <kind> errata <id> "<text>"             # kind: feature | decision | runbook | note
cerne feature errata F-0008 --file errata.md  # or piped stdin — one-shot, like note new
cerne decision errata D-0012 "<text>"
cerne note errata N-0003 "<text>"
cerne runbook errata RB-0002 "<text>"
```

The errata is **purely additive** (`content/errata/<id>.md`, one dated entry per call, in chronological order): it never touches the original `spec.md`/`plan.md`/`result.md` or ADR body. It is read back wherever the entity is consumed — shown under `cerne show <id>`, folded into the wiki synthesis (`cerne compile`), and considered by `cerne lint` (scanned regardless of state: a superseded entity an entry names is flagged as in any doc, and since no entry can be edited, a later entry carrying the marker acknowledges it — see below). The result rules count it too: a `critério N` cited in an errata entry clears `result-missing-evidence` for that N, and a complete `## Gauntlet` section in any entry clears `result-missing-gauntlet` — the path for a result debt found after the accept. Only once the feature is closed: while it is live the result itself is the document to fix, and the `complete` gate ignores the errata. There is **no** edit/rewrite command — past the accept, errata is the only correction path, by design: annotate, never rewrite.

One tolerance is automatic: an errata may **name what its own entity superseded** (e.g. F-0001's errata saying "this feature replaced F-0002") without `cerne lint` raising `ref-to-superseded` — that supersession record is legitimate history. Any **other** superseded entity an errata names is still flagged, including one that was live when the entry was written and was superseded later. No command edits an entry, so the old line can never take the same-line marker; acknowledge it by **appending** an entry that carries the marker for that id:

```
cerne <kind> errata <id> '<!-- lint:ok-superseded X --> <why the mention is history>'
```

Use single quotes: inside double quotes an interactive zsh expands the marker's `!` before cerne runs. For several ids, list them in one marker — `<!-- lint:ok-superseded X, Y -->` — since only the first marker on a line counts. The finding itself names this command, with the owner's kind and id filled in.

A marker in a later entry covers every mention of X in the entries **before** it. It never covers a later entry, and inside its own entry the same-line rule holds: keep any mention of X in the new entry on the marker's line. If the old entry is wrong rather than dated, say what is current on that same line.

## Drift: code moved, docs stood still

An `implemented` feature's docs are frozen history — but the code under them keeps changing. **Drift detection** (F-0113/F-0116) maps the project repo's git diff since the last docs sync to the implemented features whose scope it touches, deterministically (no LLM). It is **opt-in**: the project's `.cernerc` must map scopes to path globs — `[module_paths]` (module → globs, for moduled projects) and/or `[component_paths]` (component tag → globs, the axis that works in a moduleless project). No config, no baseline → the rule is silent, never noisy.

- **See it:** `cerne drift` (also folded into `cerne lint`). A scope with a *live* feature is suppressed — active work already tracks that change.
- **Fix it — three paths, pick by project policy:**
  - **Manual:** review the finding, append `cerne feature errata <id> "<text>"`.
  - **Gated draft:** `cerne drift --draft` emits one delegation prompt per finding under `.docs/.drift-proposals/`; **you** (the host agent) write the errata draft into the sibling `<id>.md`, a human applies it via `cerne feature errata <id> --file …` (F-0114).
  - **Automatic:** `cerne drift --auto` drafts via the configured `[llm]` provider (claude-cli/byo-key) and applies the errata itself under the `drift-auto` actor, then records a pending review (F-0115, D-0005 apply-then-notify).
- **Review duty (D-0005):** every automatic update waits for acknowledgment. `cerne drift pending` lists them (`cerne next` shows the count) — treat a pending entry as a work item: read the applied errata (`cerne show <id>`), then `cerne drift ack <id>` to accept, or append a correcting errata / supersede if the agent got it wrong. Never leave pendings to rot.
- **Invariant:** drift never rewrites history. All three paths end in an **additive errata**; the reading surface (`cerne show`, wiki, lint) folds erratas in, so the reader sees current truth over an intact record.

Adjacent freshness knob: with `compile_on_push = true` in `.cernerc` (and a callable `[llm]` mode), every successful `cerne push` incrementally resynthesizes only the **dirty** wiki pages (F-0117) — a failed compile never fails the push. No config → recompile stays manual (`cerne compile`).

## Knowledge routing: cerne note vs. local memory

When you acquire knowledge worth keeping, route it by **who it belongs to**, not by reflex:

- **Durable, team-relevant project knowledge → `cerne note new`.** Gotchas, invariants, hard-won conventions, "why X and not Y" — anything a teammate or your own agent on another machine would need. It belongs in the guardian, where it is shared across machines and members, versioned, indexed, and governed (so it can be superseded instead of rotting). This is the gap notes exist to close: such knowledge otherwise dies in one person's local `~/.claude/.../memory/`.
- **Personal or machine-specific knowledge → stays in local Claude Code memory, never pushed.** Who you are and how you like to work (`user`/`feedback`-class) is personal — sharing it imposes one person's style on the team. **Machine-specific** facts are worse: a memory like "Go lives at `C:\…\go\bin`" is correct on one box and **wrong** on another, so pushing it is an actively harmful asset. Leave these where they are.
- **When in doubt, ask:** would this still be true and useful for a different person on a different machine? If yes, it is a note. If it only holds for *you* or *this box*, keep it local. Never bulk-sync memory into cerne — the boundary requires this per-item judgment.

## External sources (provenance)

The moment an external source (article, RFC, vendor/API doc, spec) **shapes** a feature, decision, or fix, register it — as you use it, **without waiting to be asked** — instead of leaving a bare URL in prose. Register **only the sources that actually informed the outcome**: a dead-end you opened and discarded is not a reference. If a URL is worth citing in the doc it is worth a `cerne reference`; if it is not, don't leave it as bare prose either.

```
cerne reference new "<title>" --url <url> --informs F-NNNN,D-NNNN [--facet <f>] [--component <c>] [--file <summary> | stdin]
```

It is marked external, its content is indexed (so `cerne ask` cites it), and `--informs` backlinks it to the entity it motivated.

References carry the **same two tag axes as notes** — `--facet` (the closed `infra`/`code`/`data`/`process`/`auth` set; an invalid facet aborts before any write) and `--component` (open, slug-normalized) — and they fold into the FTS the same way, so `cerne search`/`ask` find a reference by its tag. List and filter them with `cerne reference list [--facet <f>] [--component <c>] [--all] [--json]` — the **only** listing for references (AND across axes; `--all` includes superseded; `--json` for machine reads). Legacy references predate tags, so a `--facet` filter over them returns empty with a guidance message. Re-tag a current reference in place with `cerne reference retag R-NNNN --facet <f> --component <c>` (like notes/decisions) — it sets only the axis you pass (`--facet ""` clears that axis), preserves url/informs/body, and never makes the reference obsolete. Retire one with **`cerne reference retire R-NNNN --reason "…"`** (reason required) — a duplicate or a source that no longer holds moves from `current` to `obsolete`, leaving the current state but staying reachable via `reference list --all` and search as history; once it is obsolete, a new reference with the same URL can be registered.

## Housekeeping, legacy entities, and reporting

- **Unassigned modules:** `cerne classify` (no args) dry-runs a heuristic backfill — assigns a module to a module-less feature only when exactly one declared module matches (`--yes` applies it); `cerne classify F-NNNN --module <m>` assigns one feature manually.
- **Bulk-importing an existing doc tree** (bootstrap only, not for registering a single new case): `cerne import` with no flags uses the deterministic Rockt Hub convention; `--agent` does an AI-assisted generic import for any structure (emits a mapping prompt, then `cerne import --agent --ingest [--yes]` validates and applies it). It already guards against re-minting a candidate whose title matches an existing feature.
- **Legacy entity that predates its own spec/case** (an absorbed module, a discontinued system): `cerne retire "<name>" [--alias <a>]... [--by <successor>] [--reason "…"]` registers it born `superseded`, so its declared aliases start being flagged by lint when they show up in current documents. If an alias later proves too common/ambiguous and causes false-positive `ref-to-superseded` findings, drop it with `cerne forget-alias <name> <alias>...`.
- **Full relationship graph:** `cerne graph [--format json|dot]` exports every entity and edge (links, supersessions, parent/child) for external tooling — the traceability view beyond what `.docs/CONTEXT.md`/backlinks show inline.
- **Metrics, read-only:** `cerne stats [--since <date-or-duration>] [--json]` reports lead/cycle time and monthly throughput from the event log; `cerne stats --process [--json]` — process panel (F-0321): weekly median of weighted tokens per delivered feature from the stamped `## Custo` anchors; decisions with facet `process` + component `metric` are markers, each judged before/after (28 days, ≥ 8 per side, 90% bootstrap) as `melhorou`/`piorou`/`inconclusivo` with its reason; the quality guards (Gauntlet returns, `--fixes` escaped defects, each a 97.5% score interval) veto a cheaper-but-worse change and decide alone, either way, when the cost shows no detectable difference — a verdict whose guards are pending or too thin says `guarded: false`; epics (features with children) and unstamped deliveries are excluded and counted per side, next to each side's coverage of its bound sessions; `cerne savings [--json]` reports how much documentation the backend holds versus what `.docs/` actually materializes — the context an agent is spared from reading. `cerne cost [<id>] [--json]` reports the tokens an entity consumed, read from the Claude Code session transcripts — `input` (new tokens, full rate), `cache_creation`, `cache_read`, `output`, per model, with the subagent (sidechain) share and an `unattributed` bucket for the messages of those sessions outside any binding; no monetary value, and the opposite quantity of `cerne savings`. Without an id it is the project view per entity. `cerne cost E-NNNN` (any id with children) sums the children's stamped anchors and names the children without one, never silencing the gap.

## Hard rules

- **Register the task at the start.** The first action when you pick up a task is `cerne feature new "<title>" --context "<1-3 frases do porquê>"` (it lands as a candidate, visible on the kanban, with its creation context captured — bare creation is refused) — not something you do after the work is finished. Advance it as you go (spec → `approve`, plan → `start`, result → `complete`); if it is abandoned, `cerne feature reject <id> --reason "…"` so the board stays honest. Candidates left without a real spec past the grace window are flagged by `cerne lint` (`stale-candidate`).
- **Register the sources that shaped a decision.** When an external source (article, RFC, vendor/API doc, spec) informs a feature/decision/fix, register it as you use it — `cerne reference new "<title>" --url <url> --informs <ids>` — not only when a human asks for it. Register **only the useful ones**: the sources that actually moved the outcome, never a dead-end you opened and discarded. A source worth citing in the doc is worth a reference; a bare URL left in prose is lost provenance. `cerne lint` backs this up — a **decision or feature** doc that cites an external URL with no backing registered reference is flagged **`unregistered-source`** (register the source, or drop the bare URL); internal/local hosts and any `unregistered_source_ignore` substrings in `.cernerc` are skipped. Install the proactive gate reminder with `cerne hook install`.
- **Author through cerne**, not as loose markdown folders for tracked features/decisions — loose markdown is authoring input, not authority.
- **Never hand-edit generated outputs:** `.docs/CONTEXT.md` and the compiled wiki regenerate (`cerne pull` / `cerne compile`).
- **Review pending automatic updates.** When `cerne next` reports drift updates awaiting review, handle them (`cerne drift pending` → `ack` or correct) as part of the session's work — apply-then-notify (D-0005) only stays honest if someone actually reviews.
- After a `cerne push` that changes the backend, commit the updated `docs.lock` (the code↔docs pin) alongside your code change — **unless the project sets `pin_branch` in `.cernerc` and you are on another branch** (F-0102): there cerne leaves `docs.lock` untouched on purpose (the live snapshot lives in the gitignored `.docs/.snapshot`), the pin advances on the first cerne operation on the pin branch after merge, and MRs stop conflicting on it. Don't "fix" an unchanged `docs.lock` by hand.
- `.docs/` is gitignored; commit `.cernerc`, `docs.lock` **and `.gitattributes`** in your project repo. The `.gitattributes` wires the `docs.lock` merge driver that `cerne init`/`cerne pull` register, so parallel branches/worktrees stop conflicting on the snapshot pin — a fresh clone just runs `cerne pull` once to self-heal its local git config. Server-side merges (GitLab/GitHub MRs) can't run that driver; setting `pin_branch = "main"` removes the conflict at the source instead.
