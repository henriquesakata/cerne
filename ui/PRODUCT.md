# Product

<!-- impeccable:product-schema 1 -->

Scope: the **cerne web UI** (`ui/`, served by `cerne ui` at `localhost:7717`).
Written in English to match the repo convention that AI-facing docs are English
(the UI's own strings are pt-BR — see Brand Commitments).

## Platform

web

## Users

Three confirmed audiences, all human. The UI is **not** an agent-consumption
surface — agents read `.docs/CONTEXT.md` and the CLI (`cerne brief`, `status`,
`next`, `ask`), which are the token-lean paths built for them.

- **Developer reviewing state.** Opens the UI to see the project's real roster:
  what exists, in which lifecycle state, what is stalled. Situation: a check-in,
  or picking up the next task.
- **Developer operating human gates.** `cerne feature approve` and
  `cerne feature accept` are human acts by default (F-0131); agents are refused
  when `CERNE_ACTOR=agent`. The UI is where that human reads the spec and
  exercises the gate. Situation: an agent has asked for approval and is blocked.
- **Non-developer stakeholder.** A PM or lead who does not run the CLI, tracking
  delivery. Reads rollups (epics, flow metrics, activity), not raw data.

The first two are dense-reading power users; the third is not. The surface must
serve both without forking into two products.

## Product Purpose

cerne manages the living documentation of a software project: it separates what
is **current** from what is **history**, materializes only the current state into
a gitignored `.docs/` folder that AI agents read, and keeps the lifecycle of
features, decisions, notes, runbooks, and references as **structured data,
never prose**.

The web UI is the **human window** onto that data. The CLI and `.docs/`
projection serve agents; the UI serves the people who must trust, correct, and
approve what the agents did. Success: a human opens the UI and knows the true
state of the project without reading a hand-maintained roadmap — because no such
roadmap exists.

## Positioning

Documentation state is a **gated finite-state machine over structured entities in
a git backend**, not markdown files someone remembers to update. Lifecycle moves
only through commands that enforce the transition (`feature new|approve|start|
complete|reject`), each writing an event to an append-only log. A stale roadmap
is not a discipline failure to be nagged about — it is made structurally
impossible, because the roster is derived, never authored.

The neighboring products (a docs site generator, a wiki, a kanban board) cannot
truthfully copy this: they store prose or store state, not state that *generates*
prose on demand while keeping the full superseded history queryable.

## Operating Context

- Runs **locally**, on the developer's own machine, from a single Go binary
  (`cerne ui` → `localhost:7717`). No hosted instance, no login, no multi-tenant
  surface. The UI is embedded in the binary via `go:embed ui/dist`.
- The backend of truth is a **separate git repository** (`cerne-docs`), pinned by
  a committed `docs.lock`. The UI reads a derived SQLite FTS5 index.
- Used **alongside** an AI coding session, not instead of it: the typical moment
  is an agent working in the terminal while the human has the UI open in a
  second window to watch the board move and clear gates.
- This repo **dogfoods itself** — cerne's own documentation is managed by cerne,
  so the maintainers are also its heaviest daily users.

## Capabilities and Constraints

Confirmed surfaces (routes present in `ui/src/`): dashboard, kanban, backlog,
decisions, notes, runbooks, references, wiki, graph, mindmap, activity, history,
stats, search, ask, lint, conventions, manage.

Constraints that future design work must preserve:

- **Local-first, no network identity.** No auth, no accounts, no avatars, no
  presence. Design for a single trusted operator.
- **Ships as embedded `dist/`.** A UI change lands only if `ui/dist` is rebuilt
  *and* committed, and the canonical build platform is CI Linux (`node:24`) —
  a foreign dist fails the drift gate (F-0136). Bundle weight is not free:
  it is inside the distributed binary.
- **Lifecycle states are English strings in data** (`candidate`, `approved`,
  `in_development`, `implemented`, `rejected`, `superseded`); pt-BR labels exist
  only in the UI layer.
- **Heavy embedded viewers already exist**: Excalidraw, Mermaid, React Flow
  (`@xyflow/react`) + elkjs. Excalidraw is lazy-loaded on purpose.
- **A mature token system is in force.** `ui/src/app.css` defines the full light
  palette on `:root` and the dark overrides on `[data-theme="dark"]`, with an
  explicit rule that no theme color may exist outside those two blocks. Theme is
  set on `<html>` before first paint by an inline script.

Explicitly undecided: whether the UI ever gets a non-local deployment mode.
Nothing in the current design should assume one.

## Brand Commitments

- **Name:** cerne (lowercase). Portuguese for the heartwood of a tree — the dense
  core that carries the record of the trunk's growth. The name is load-bearing
  for the product's thesis (current state vs. accumulated history), not
  decorative.
- **Language split is deliberate and binding.** CLI output, cobra help, and error
  messages are **English** (F-0124). The **React UI strings are pt-BR** — the
  pt-BR dictionary in `ui/src/i18n.tsx` is the source of truth, with an English
  toggle available (F-0125). Design work must not "fix" the UI into English.
- **Vocabulary is domain-specific and must be kept:** feature, decision, note,
  runbook, reference, épico, gate, lint, backlog. Terms map 1:1 to CLI commands
  and data — renaming a UI label silently breaks the shared vocabulary between
  the human and the agent.

No logo, wordmark, or brand asset set exists yet. Do not fabricate one.

## Evidence on Hand

- Real production data: this repo's own cerne backend (`cerne-docs`) — a live
  roster of features, decisions, notes, events. Any screen can be designed
  against true content, never lorem ipsum.
- First external client: the **Rockt Hub**, dogfooding against `hub-docs`.
- The founding spec and result live at
  `docs/process/cases/0001_founding-spec/` (frozen history).
- **Absent — must not be invented:** customer logos, testimonials, pricing,
  user counts, benchmarks, uptime or adoption claims, press. There are no
  external users beyond the two projects above.

## Product Principles

1. **Derived, never authored.** Anything the UI shows about project state must
   come from backend data. A screen that invites a human to type freeform status
   prose is a regression to the problem cerne exists to kill.
2. **The human's job here is judgment, not data entry.** The CLI and agents do
   the writing. The UI's density and affordances should be tuned for *reading
   fast and deciding* — spec review, gate approval, spotting the stalled item.
3. **Two reading speeds on one surface.** The daily driver scans a dense roster;
   the stakeholder needs the rollup to be legible without training. Serve the
   second with hierarchy and summary, not with a separate simplified mode.
4. **Local and single-operator is a freedom, not a limitation.** No auth chrome,
   no collaboration affordances, no empty social scaffolding. Spend that space
   on the data.
5. **Weight is charged to the binary.** Every dependency and asset ships inside
   the distributed Go binary. Prefer the token system and native platform
   features over adding to `ui/dist`.

## Open Decisions

- **Audience breadth is undecided.** CLAUDE.md describes cerne as
  "open-source-in-potential." Whether it is published publicly or stays internal
  to Rockt has **not** been decided. Consequence: no public-facing marketing
  surface, landing page, or external-audience voice should be designed or
  assumed until this is settled. Record it as open; do not resolve it by
  inference.
