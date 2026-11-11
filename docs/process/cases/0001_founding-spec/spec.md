> **Translated from Brazilian Portuguese.** Historical record: this document reflects the state of the project when it was written, not the current one. For the current state, see the README and `cerne status`.

# Case 0001 — cerne founding spec

tags: #spec #cerne #founding #docs-as-code #ai-agents

**Status:** Done — v1 shipped (v0.2.0, 2026-06-12); see [result.md](result.md)
**Type:** Founding spec — defines the product, the architecture, and the v1 cut.
**Origin:** Brainstorming on 2026-06-12, driven by a real pain in the Rockt Hub: AI agents getting lost in historical documentation and citing structures that no longer exist.

---

## Problem

Software projects developed with AI agents accumulate **append-only** documentation: specs, plans, results, ADRs, and notes keep growing, but nothing mechanically distinguishes what is **current** from what is **history**. Consequences observed in the reference project (Rockt Hub, ~6 months of intensive development with agents):

1. **Scale** — ~1,000 markdown files (786 in `docs/`, 226 in `apps/*/docs/`), including 241 cross-app cases and 46 ADRs. No agent can (or should) read all of that to get context.
2. **State is prose, not data** — "which features were created, which were implemented, which are pending?" has no answer without reading dozens of files and interpreting free text.
3. **Manual indexes lie** — the hub's `features.md` still lists Bills and Investments as separate modules, although both were absorbed by Finance. The document that should be the map of the current state is itself a source of obsolete information. Agents that read it "learn" the wrong structure and propagate it.
4. **History and current carry the same weight** — a rejected case from 2025 costs the same reading tokens as the spec approved yesterday. The agent has no way to tell the difference.

The problem is not specific to the hub: **any project that uses AI in development and documents the process will end up here**. Existing tools don't solve it:

- **RAG** retrieves relevant snippets but has no notion of state (it retrieves the obsolete with the same confidence as the current).
- **Jira/Notion/Confluence** have state, but no consumption layer for agents (they don't materialize context into files) and no binding to the code repository.
- **LLM Wiki (Karpathy, 2025)** proposes the compiled synthesis layer maintained by an LLM, but has no structured lifecycle events — obsolescence detection depends on LLM lint passes.

**cerne's thesis is the union of the three:** structured lifecycle (state as data) + a compiled layer for agent consumption (wiki) + materialization of the current state into files in the user's repo.

> The name: *cerne* is the hard heartwood at the center of the tree — the durable part that remains while the tree grows. The tool preserves the project's *cerne*; the rest is queryable history.

---

## Product vision

**cerne** is a platform-independent tool (CLI + local UI, a single binary, potentially open source) that manages the living documentation of a software project:

- The user's repository gains a **materialized, gitignored `.docs/` folder** containing *only the current state*: active specs, active ADRs, current references, and a compiled wiki. It is what agents read — natively, with no protocol.
- The **full history** lives in cerne's backend (v1: a separate git repository), with local full-text search.
- **State transitions** (approve, reject, supersede) are validated tool operations — never text edits.
- A **committed `docs.lock`** binds each state of the code to the corresponding docs snapshot.

Audiences: teams developing with agents (Claude Code, Cursor, etc.) **and** non-technical/hobbyist creators — for them, the tool's opinionated flow IS the methodological guide ("your spec was approved; the next step is the plan").

The Rockt Hub is the first client (dogfooding via `cerne import`); the Rockt platform's Studio will, in the future, be a client of the cerne API for platform projects. cerne **does not depend** on the platform.

---

## Decisions (validated in brainstorming)

| # | Decision | Discarded alternatives |
|---|---------|--------------------------|
| 1 | **Independent product**, potentially open source; the hub is the first adopter | Internal hub tool; Studio module |
| 2 | **Hybrid files + structured state**: content in markdown, state graph as first-class data; materialized `.docs/` (gitignored) + committed `docs.lock` + remote backend with history | Pure files-first (state becomes prose again); database-first (lock-in, agent held hostage by an API) |
| 3 | **Hybrid writing**: content is edited as files + `push`; state transitions only via a validated command | Everything via files (no consistency); everything via API (friction for agents) |
| 4 | **CLI-first**; MCP as a thin shell in v2 | MCP-only (restricts clients); CLI+MCP simultaneously in v1 (scope) |
| 5 | **Fixed, opinionated lifecycle in v1**: the tool is the methodology. Feature as the central entity | Jira-style configurable workflow (a workflow engine ≠ an opinionated product) |
| 6 | **Deterministic core + pluggable LLM**: materialization, lifecycle, structural lint, and search work without an LLM; wiki compilation and Q&A are optional layers | Mandatory embedded LLM (cost + barrier for hobbyists) |
| 7 | **Local web UI served by the binary itself** (`cerne ui`); Excalidraw embedded for visual spec review | No GUI (hobbyists don't live in the terminal); SaaS from day 1 (premature infra) |
| 8 | **v1 backend = git-as-backend** (a separate git repo managed by cerne): zero infra, auditable history for free | Self-hosted server or SaaS in v1 (both become future backends behind the same interface) |

---

## Domain model

Six entity types. All have a **stable ID** and **status as data** (never prose):

| Entity | Role | States |
|---|---|---|
| **Project** | Root; defines modules/scopes (e.g., finance, identity) | — |
| **Feature** | Central entity (work item) | `candidata → aprovada → em-desenvolvimento → implementada`; alternative terminals: `rejeitada`, `substituída` |
| **Document** | Spec, plan, or result — always belongs to a Feature (they are its stages) | derived from the Feature's state |
| **Decision (ADR)** | Architectural decision | `ativa / substituída` |
| **Reference** | Architecture, runbooks, guides | `vigente / obsoleto` |
| **Artifact** | `.excalidraw` diagrams, images — attached to Documents | — |

**Relations with semantics:** `superseded_by` carries a reason + link (e.g., Feature Bills → `superseded_by: finance-absorption, reason: "absorvida pelo Finance"`). ADRs link to the Features that motivated them. This is what turns "a footnote agents ignore" into a structured record the lint uses.

**Lifecycle gates (fixed in v1):**

- `aprovar` requires a spec to be present
- `iniciar desenvolvimento` requires a plan
- `concluir` requires a result

**Append-only event log:** every transition becomes an event (`feature.approved`, `feature.superseded`, `adr.superseded`, …). The log feeds:

1. **Deterministic lint** — "Bills was superseded → N pages reference it → flag them". No LLM.
2. **Wiki invalidation** — affected compiled pages are marked dirty for recompilation.

---

## Architecture

A single binary, four layers:

```
┌─────────────────────────────────────────────────┐
│  Interfaces:  CLI  │  UI web local  │  (MCP v2) │
├─────────────────────────────────────────────────┤
│  Núcleo determinístico                          │
│  lifecycle · materialização · lint · busca FTS  │
├─────────────────────────────────────────────────┤
│  LLM plugável (opcional)                        │
│  compilação de wiki · Q&A                       │
├─────────────────────────────────────────────────┤
│  Storage                                        │
│  cache local (SQLite) ←sync→ backend remoto     │
│  (git-backend v1 │ self-hosted v2 │ SaaS v2+)   │
└─────────────────────────────────────────────────┘
```

**Footprint in the user's project:**

| Item | Versioned? | Content |
|---|---|---|
| `.docs/` | no (gitignored) | materialization: current docs + wiki + `CONTEXT.md` |
| `docs.lock` | **yes** | snapshot-id of the docs state ↔ code state |
| `.cernerc` | yes | config: backend, default scope, project name |

The `docs.lock` fixes the biggest risk of the "docs outside git" model: without it, an old checkout doesn't know what the docs state was, and PRs don't bind code to spec. With it, `cerne pull` rebuilds the materialization of any commit (analogy: `package.json`/lockfile vs `node_modules`).

### Materialization layout

```
.docs/
├── CONTEXT.md            ← ponto de entrada único, gerado, com orçamento de tokens
├── wiki/                 ← síntese compilada: 1 página de visão por módulo
└── current/
    ├── features/         ← só candidatas/aprovadas/em-desenvolvimento (spec/plano)
    ├── decisions/        ← só ADRs ativas
    └── references/       ← só docs vigentes
```

**Contract with the agent (1 paragraph in the project's CLAUDE.md):** *"Current project state in `.docs/` — start at `CONTEXT.md`. Do not cite documentation outside `.docs/`. History via `cerne search` / `cerne ask`."*

**Promotion on completion (anti-growth rule):** spec, plan, and result are *process* documents. When the Feature becomes `implementada`, they leave the materialization (they stay in history); durable knowledge is promoted (decisions → ADR, behavior → Reference, vision → wiki). The current set does not grow with the project's age — it grows only with its current size.

---

## Commands (v1)

| Command | Function |
|---|---|
| `cerne init` | new project: creates backend, lock, `.docs/` |
| `cerne import` | existing repo: scans docs, proposes a mapping (case→Feature, adr→Decision), populates the backend |
| `cerne pull [--scope X]` | materializes `.docs/` (optional slice); idempotent |
| `cerne push` | validates schema/links, writes to the backend, updates the lock, incremental lint |
| `cerne status` | table of features by state |
| `cerne feature new "título"` | creates a `candidata` Feature |
| `cerne feature approve F-042` | validates the gate (does the spec exist?), records an event |
| `cerne feature supersede F-007 --by F-042 --reason "..."` | structured supersession |
| `cerne lint [--fix-tasks]` | deterministic report: references to superseded entities, broken links, orphans, pending gates; `--fix-tasks` emits tasks for the host agent to execute |
| `cerne search "query"` | local FTS over the full history |
| `cerne ui` | opens the local dashboard |

**Conflicts:** `push` detects remote drift (outdated lock) and requires a `pull` first — a git-like model. Content is markdown and the v1 backend is git: hard merges fall back to existing tools.

**v1.5 (pluggable LLM):** `cerne compile` (recompiles dirty wiki pages) and `cerne ask "pergunta"` (Q&A: answers from the wiki, dives into history if needed). Modes: BYO API key or delegation to the host agent.

---

## Local UI (v1)

`cerne ui` serves on `localhost`:

1. **Feature kanban by state** — a visual answer to "what is created/implemented/pending"
2. **Feature page** — spec/plan/result tabs, diff against the last approved version
3. **Approval screen** — spec + diagrams side by side; approve/reject with a mandatory reason (becomes an event)
4. **Lint report** and event timeline

**Excalidraw:** open-source React component, embedded. The agent generates the `.excalidraw` JSON (sequence diagram, architecture) as an Artifact attached to the spec; the human views and edits it in the embed before approving.

---

## Version cut

| v1 (MVP) | v1.5 | v2+ |
|---|---|---|
| CLI: init, import, pull, push, status, lifecycle, lint, search | `compile` + `ask` (pluggable LLM) | MCP server |
| Git backend + lock + materialization | | Self-hosted server / SaaS |
| UI: kanban + spec review + approval w/ Excalidraw | | Studio integration (Rockt) |

---

## Success criteria

1. **Real acceptance test — `cerne import` of the Rockt Hub:** imports 241 cases + 46 ADRs proposing a correct mapping; the lint flags, unassisted, the references to Bills/Investments as superseded entities (the real `features.md` bug).
2. **Context reduction:** an agent in the hub goes from ~1,000 visible files to dozens (active features + active ADRs + current references). The "agent cites Bills as a live module" case dies by construction.
3. **`cerne status` answers in 1 command** what today requires reading dozens of files.
4. **E2e golden path:** `init → feature new → push spec → approve → pull` works on a pristine repo in < 5 minutes, with no LLM and no infra beyond a git repo for the backend.
5. **Traceability:** checking out an old commit + `cerne pull` rebuilds the materialization from that time.

---

## Testing strategy

- The deterministic core pays off here: lifecycle gates, materialization snapshots, and lint rules are pure unit/integration tests, **with no LLM mocking**.
- E2e of the golden path (criterion 4).
- Continuous dogfooding: cerne's own repository uses cerne (this case 0001 will be imported as Feature F-001 once `import` exists).

---

## Open questions

1. **Stack** — the "one binary, CLI + embedded UI + SQLite" requirement points to **Go or Rust**; decide in plan.md (criteria: iteration speed, embedding web assets, SQLite FTS, cross-platform distribution).
2. **License and language** — if open source: license (MIT/Apache-2.0?) and the language of the public documentation (English) vs process docs (pt-BR today). Decide before the first public release.
3. **Name on GitHub** — npm `cerne` free as of 2026-06-12; check the GitHub org/repo when creating the remote.
4. **Multi-user** — v1 assumes one user/agent at a time per project (optimistic lock). The simultaneous collaboration model is left for the v2 server.
5. **Granularity of the `CONTEXT.md` token budget** — initial heuristic vs configurable.

---

## Next steps

1. `plan.md` in this case (`writing-plans` skill): decide the stack, the repo skeleton, the TDD implementation order (suggestion: lifecycle/state → lock/materialization → import → lint → search → UI).
2. Implement v1 with the hub as the `import` guinea pig from early on.
3. `result.md` on completion; this spec's status → Done.
