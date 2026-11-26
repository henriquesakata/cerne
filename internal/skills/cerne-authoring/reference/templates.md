## Spec structure (the template cerne scaffolds)

cerne scaffolds a new feature's `spec.md` from a canonical template (the same one `cerne spec draft` asks the model to fill). Fill each section; the lifecycle gates treat the empty scaffold as a stub until you do. When the design benefits from a sequence diagram, `cerne spec diagram F-NNNN [--name <name>]` AI-drafts a Mermaid diagram artifact for it (delegation prompt, or generated directly in byo-key/claude-cli `[llm]` mode). Sections, **in order**:

1. **Problem** (pt-BR: Problema) — what is missing or broken **today**, grounded in real code (`file.go:line`); why now; the cost of not fixing it. Problem only — no solution yet.
2. **Goal and acceptance criteria** (pt-BR: Objetivo e critérios de aceitação) — one objective sentence, then **numbered, testable** criteria (1 criterion : 1 test). For behaviour, use EARS — *"WHEN `<condition>` THE SYSTEM SHALL `<behaviour>`"* (pt-BR: *"QUANDO `<condição>` O SISTEMA DEVE `<comportamento>`"*); structural invariants as a direct, measurable assertion.
3. **Design decisions** (pt-BR: Decisões do design) — a `| Fork | Decision | Reason |` table (pt-BR: `| Fork | Decisão | Razão |`); record the rejected alternatives, not just the chosen path.
4. **Design** (pt-BR: Desenho) — by module/package, grounded in real files, with explicit contracts (signatures, schemas, endpoints, events). Include the *how* — don't strip it out.
5. **Verification** (pt-BR: Verificação) — the test plan (TDD), 1:1 with the acceptance criteria.
6. **Out of scope** (pt-BR: Fora de escopo) — what you will **not** do now, and why (or what becomes a follow-up).

**Ordering is deliberate, not cosmetic.** Problem and the acceptance criteria sit at the top and Verification near the end (pt-BR: Problema, Verificação), so the most decision-bearing, testable content lands where long-context models attend best (primacy/recency) rather than buried in the middle. Keep it dense — a bloated spec hurts more than it helps.

**Mark ambiguity, don't guess.** Wherever something is unresolved, leave `[CLARIFY: specific question]` (pt-BR: `[ESCLARECER: pergunta específica]`) instead of a plausible-but-maybe-wrong guess, and resolve every marker before `cerne feature approve`. **Write the marker in the doc's own language** — the vocabulary is a per-language catalog (pt `[ESCLARECER:]`/`[ESCLARECIDO:]`, en `[CLARIFY:]`/`[CLARIFIED:]`) and cerne recognizes all of it everywhere (UI chips, tab badge, lint, approve gate), so an English doc uses `[CLARIFY: specific question]` and gates exactly like a pt-BR one. Options work the same in any vocabulary (`[CLARIFY: where? | doc | sidecar]`), and answering via the UI writes the resolved form in the marker's own vocabulary.

**Ask with options (F-0517).** In an English project: clarification options: when a question has possible answers, write each one after the question, one per pipe, and end the one you would pick with (recommended) (at most one; the reason goes in the question): [CLARIFY: <specific question> | <option A> (recommended) | <option B>]; a question with no possible answers stays without a pipe. The person answers with one click and can always write another answer.
In a pt-BR project: clarification options: when a question has possible answers, write each one after the question, one per pipe, and end the one you would pick with (recomendada) (at most one; the reason goes in the question): [ESCLARECER: <pergunta específica> | <opção A> (recomendada) | <opção B>]; a question with no possible answers stays without a pipe. The person answers with one click and can always write another answer.

`cerne clarify list` shows the options recommended first, `--pick <k>` follows that order, and `cerne spec check` / `cerne feature approve <F-id> --check` advise (never block) on a marker with no options.

The evidence behind this structure (lost-in-the-middle / primacy-recency, EARS, ADR, spec-driven development) is registered in the backend as external references — `cerne ask` cites them.

### Resolving `[ESCLARECER]`/`[CLARIFY]` markers from the terminal

Instead of hunting markers by hand across `.docs/`, work the queue directly: `cerne clarify list [<id>] [--json]` lists every open marker across live entities (or just one entity's); `cerne clarify answer <id> <n> --answer "…"` (or `--pick <k>` to choose one of the marker's proposed options) rewrites the n-th open marker to its resolved form (`[ESCLARECIDO: question → answer]`) and pushes it through the standard pipeline. Answering renumbers the open markers after it; the commands the approve refusal prints carry `--expect <key>`, a fingerprint of the marker they were printed for, so a command whose numbering moved is refused and nothing is written — re-run `cerne feature approve <id>` for fresh numbers. Got an answer wrong after the fact? `cerne clarify list --resolved` shows the correction queue, and `cerne clarify correct <id> <n> --answer "…"` appends a corrected answer to that marker's trail (`question → previous → new`) — the prior answer is kept, not erased.

## Plan structure (the template cerne scaffolds)

cerne scaffolds `plan.md` from a canonical template when you **approve** a feature (the same one `cerne plan draft` asks the model to fill) — the execution counterpart of the spec. A plan is the bridge from an approved spec to code: it says **how** to build, never re-stating **what** (link the spec, don't repeat it — context is finite). **Right-size it:** a trivial change is a single task, and sections that don't apply collapse to one line — don't inflate a small plan with ceremony. Sections, **in order**:

1. **Strategy** (pt-BR: Estratégia) — the approach in 2-3 sentences and how the work is sliced (single MR or N phases) and why. Point at the spec; don't duplicate it.
2. **Task sequence** (pt-BR: Sequência de tarefas) — small, **ordered** tasks, each leaving the repo green. Per task: real file paths (create/edit/delete), **test first** (TDD) then the minimal implementation, a **verification** command (→ expected output) **citing the spec criterion number** it satisfies, and the exact Conventional commit message. Mark `[parallel]` (pt-BR: `[paralela]`) when a task is independent.
3. **Dependencies and order** (pt-BR: Dependências e ordem) — what unblocks what (the real order, not just a list), which tasks are parallel-safe, and external dependencies (other cases, services, migrations).
4. **Risks and rollback** (pt-BR: Riscos e rollback) — per risk: how to detect + how to mitigate; and how to revert (each committed task is a return point) — flag destructive or irreversible steps.
5. **Constraints for the agent** (pt-BR: Restrições para o agente) — execution guardrails: what **not** to touch, decisions already settled in the spec to respect, and where to **pause** for human review. Literal instructions — the agent follows them to the letter.
6. **Definition of done** (pt-BR: Definição de pronto) — a **binary** checklist (each item MET/UNMET with `file:line` evidence, not "looks done"): every spec criterion has both an implementation and a test that proves it, tracked by number; full suite green; result.md written. It is the `cerne feature complete` gate.

**Ordering is deliberate, not cosmetic.** Strategy and Task sequence sit at the top and Definition of done at the end (pt-BR: Estratégia, Sequência de tarefas, Definição de pronto), so the most decision-bearing, executable content and the exit gate land where long-context models attend best (primacy/recency). **Mark ambiguity, don't guess** — leave `[CLARIFY: specific question]` (pt-BR: `[ESCLARECER: pergunta específica]`) and resolve every marker before `cerne feature start`. The evidence behind this structure (context engineering / lost-in-the-middle, plan-and-solve decomposition, spec-driven plan→tasks, agentic-coding best practices) is registered in the backend as external references.

## Result structure (the template cerne scaffolds)

cerne scaffolds `result.md` from a canonical template when you **start** a feature (the same one `cerne result draft` asks the model to fill, with the spec and plan as context). The result **closes the loop**: the spec sets the acceptance criteria, the plan defines done, and the result **verifies done with evidence**. Record what shipped, not what was planned; **show evidence, never assertion**. **Right-size it** — a small delivery has short sections. Sections, **in order**:

1. **Result** (pt-BR: Resultado) — what shipped and the state (delivered / partial), in 1-2 sentences. The bottom line first.
2. **Acceptance** (pt-BR: Aceitação) — each numbered spec criterion → **MET/UNMET (ATENDIDO/PENDENTE)** with **evidence** (test name, `file:line`, numbers). Binary and 1:1 with the spec — no "looks done".
3. **What changed** (pt-BR: O que mudou) — the concrete changes from the reader's perspective: files/modules created and edited, new commands, observable behaviour.
4. **Debts and follow-ups** (pt-BR: Débitos e follow-ups) — what's incomplete or deferred, as concrete work (a new feature/case), not an orphan TODO. Register each known debt as a pending (`cerne pending new … --from F-NNNN`, see **Pendings**) and cite its `P-NNNN` here — prose alone is forgotten the day after `complete`.
5. **Learnings and deviations** (pt-BR: Aprendizados e desvios) — what you learned and any intentional deviations from the spec/plan (what changed and why). Concise; "N/A" if nothing notable.

**Ordering is deliberate** (primacy/recency): Result and Acceptance lead (pt-BR: Resultado, Aceitação) — the outcome and its proof — and the softer learnings sit last. **Mark ambiguity, don't guess** — `[CLARIFY: specific question]` (pt-BR: `[ESCLARECER: pergunta específica]`). The evidence behind this structure (Definition of Done, show-evidence-not-assertion, binary acceptance verification, Keep a Changelog) is registered in the backend as external references.

## ADR structure (the template cerne scaffolds)

cerne scaffolds `content/decisions/D-NNNN.md` from a canonical ADR template when you create a decision (the same one `cerne decision draft` asks the model to fill). It follows Michael Nygard's classic spine (Context → Decision → Consequences), extended with the alternatives and code anchors that make a record durable. **Right-size it:** a small decision can leave the last sections as one line ("N/A") — don't inflate. Sections, **in order**:

1. **Context** (pt-BR: Contexto) — the forces and constraints in tension and the trigger (why decide now). Name the forces; ground them in real code/cases.
2. **Decision** (pt-BR: Decisão) — the choice stated imperatively ("We will X" / "We use X"; pt-BR: "Vamos X" / "Usaremos X") with the two or three reasons that won.
3. **Consequences** (pt-BR: Consequências) — what follows: positives, **negatives/trade-offs (always list them)**, and constraints/invariants (what cannot change without revisiting).
4. **Alternatives considered** (pt-BR: Alternativas consideradas) — the options weighed (2+ when there is a real trade-off), each with pros/cons and **why it was rejected**.
5. **Implementation and relations** (pt-BR: Implementação e relações) — where the decision shows up in code (real files/modules) and the links: the features and ADRs it affects, supersedes, or that motivated it (use F-/D- ids).

**Ordering is deliberate** (primacy/recency): Context and Decision lead; Implementation and relations is lookup material at the end (pt-BR: Contexto, Decisão, Implementação e relações). **The title records the decision, not the question** ("Use Postgres for primary storage", not "Which database?"). **Status is data, not prose** — the lifecycle is owned by cerne (`cerne decision supersede`), so the template has no Status section, and **an accepted ADR is a historical record: to change course, supersede it — don't rewrite it.** **Mark ambiguity, don't guess** — leave `[CLARIFY: specific question]` (pt-BR: `[ESCLARECER: pergunta específica]`) and resolve it before treating the decision as accepted. The evidence behind this structure (Nygard's ADRs, MADR, ThoughtWorks' lightweight ADRs, the create-adr skill) is registered in the backend as external references.

## Runbook structure (the template cerne scaffolds)

cerne scaffolds `content/runbooks/RB-NNNN.md` from a canonical template when you create a runbook (the same one `cerne runbook draft` asks the model to fill). A runbook is read mid-incident, so every step is a command with its **expected output**, never a paragraph. **Right-size it** — a simple procedure has short sections. Sections, **in order**:

1. **When to use** (pt-BR: Quando usar) — the trigger, symptoms or goal: the situation that fires this runbook, so the reader confirms in seconds it is the right one.
2. **Prerequisites** (pt-BR: Pré-requisitos) — access/tokens, tools and versions, environment state, window/approvals to confirm before starting.
3. **Procedure** (pt-BR: Procedimento) — numbered, action-first steps with copy-pasteable commands, each with its expected output ("you should see: X"; pt-BR: "você deve ver: X"); note idempotency where a step is safe to repeat.
4. **Verification** (pt-BR: Verificação) — how to confirm success: observable checks/signals with expected output.
5. **Rollback and troubleshooting** (pt-BR: Rollback e troubleshooting) — how to undo/recover (flag destructive steps), common failures as a symptom → cause → solution table, and **who to escalate to** when the runbook does not resolve.

**Ordering is deliberate** (primacy): the trigger, prerequisites and procedure lead — what an operator needs first under pressure. **Mark ambiguity, don't guess** — `[CLARIFY: specific question]` (pt-BR: `[ESCLARECER: pergunta específica]`). The evidence behind this structure (Google SRE playbooks, PagerDuty incident response, lightweight runbook practice) is registered in the backend as external references.
