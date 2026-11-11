> **Translated from Brazilian Portuguese.** Historical record: this document reflects the state of the project when it was written, not the current one. For the current state, see the README and `cerne status`.

# Case 0001 — Result

tags: #result #cerne #v1

**Spec status:** Done (v0.2.0, 2026-06-12)
**Release tags:** `v0.1.0` (CLI core) · `v0.2.0` (complete local UI)
**Suite:** 227 green Go tests across 16 packages · clean `go vet` · GitLab CI (vet + test)

---

## What shipped

The entire v1 cut of the spec, across the 7 milestones of plan.md:

| Milestone | Delivery | Key commits |
|---|---|---|
| M0 | Go module + cobra + GitLab CI | `c4ed905`, `acb94ff` |
| M1 | Domain: Feature FSM (6 states), per-document gates, NDJSON events, Decision/Reference lifecycles | `e155bfa`…`4882f24` |
| M2 | Storage: atomic JSON entities, backend layout, monthly event log, `NextID`, pure git operations (go-git, deterministic `main` branch, empty-remote) | `4e5c705`…`2e08445` |
| M3 | Core CLI: `.cernerc` + `docs.lock`, `init` (with joining an existing backend), `feature new` + 5 gated transitions, `status` (`--all`/`--json`) | `8f3ec24`…`b9512e0` |
| M4 | `pull` (materialization of the current state + deterministic `CONTEXT.md` + sha256 manifest) and `push` (write-back via `cerne_id`, drift detection); **e2e golden path** | `07db5a4`, `f61bf6f`, `e103c82` → **v0.1.0** |
| M5 | `import` (hub conventions: cases→Features, ADRs→Decisions, runbooks/architecture/planning→References; state heuristics; dry-run by default) and `lint` (4 deterministic rules + `--fix-tasks`) | `78abc33`, `ec8e552` |
| M6 | `search` (pure-Go SQLite FTS5, full history), embedded local UI (`cerne ui`): kanban, feature page with markdown tabs + human transitions, lazy-loaded **Excalidraw** with diagram creation/editing/saving | `f4a417e`…`c597774` → **v0.2.0** |

## Real-world acceptance (spec criteria)

Run against the **real Rockt Hub** (~1,029 md files):

1. **`cerne import` of the hub** ✅ — 308 features + 45 ADRs + 66 references in 37s (dry-run in 4.4s); states: 285 implemented, 15 candidate, 6 in_development, 2 superseded.
2. **Context reduction** ✅ — `.docs/` materialized with **138 files vs 1,029 (-87%)**; only 21 active features have materialized content.
3. **`cerne status` in 1 command** ✅ — 6 in development / 15 candidates / 285 implemented, instant.
4. **E2e golden path** ✅ — `init → feature new → push spec → approve → pull` covered by an automated test, with no LLM and no infra beyond a bare git repo.
5. **Traceability** ✅ — `docs.lock.snapshot` == backend HEAD verified across all flows.
6. **Deterministic lint** ✅ with a nuance — **204 issues in 0.5s**: 18 ref-to-superseded (active ADRs citing the superseded case 0090 and ADR-045 by their original identifiers), 25 broken links, 161 "implemented" cases with no real result.md. **Bills/Investments nuance:** the absorption predates the hub's case convention — there is no "Bills" case to become a superseded entity, so there is no needle to flag the mentions in `features.md`/`R-0010`. The *mechanism* is proven (18 real findings); the specific case requires a historical entity registry (see Open items).

## Decisions made during implementation

- **Go 1.26** (vs Rust) — recorded in plan.md; it proved right: go-git + modernc/sqlite + go:embed covered everything without cgo; a single 33 MB binary with the UI.
- **English states in the data**, pt-BR labels only in the UI.
- **SQLite as a derived cache** (`<cache>/index.db`), rebuildable; the git backend is the single source of truth.
- **Shared `CommitAndSnapshot`** (`internal/app`) — transitions, push, and artifacts use the same commit+push+lock path.
- **Committed `ui/dist`** (7.9 MB) — guarantees `go build` = complete binary from a clean clone; revisit once there is a release pipeline.
- Adjustments driven by the real-world acceptance: `docs/planning` imported as References; lint needles derived from `imported_from` (original identifiers); diacritic transliteration in slugs; BOM/CRLF tolerated in stub detection.

## Deviations from the spec

- `cerne reject` also writes `Reason` to the entity (the spec only required it on supersede) — information too valuable to live only in the event.
- `--module` validation: required/validated only when the project declares 2+ modules.
- ADR import heuristic: 45 of the hub's 46 ADRs recognized (1 outside the `adr-NNN-*` naming pattern); acceptable, documented.

## Open items and debts (for the next cases)

1. **Historical entity registry** — there is no way to declare "X existed and was absorbed by Y" when X predates the documentation (the Bills/Investments case). Ideas: `cerne feature new --superseded-by ... --historical` or seeding aliases at import. It is what's missing to fully close criterion 1.
2. **Lint doesn't read wiki-links** (`[[nota]]`, the Obsidian pattern used in the hub) — only inline `[x](y)` links; partial broken-link coverage in Obsidian vaults.
3. **`ui/dist` in git** — replace with a UI build in CI once there is a cross-platform release pipeline.
4. **Push doesn't create new documents** — docs are born via `feature new`/backend; creating a doc directly in `.docs/` is not supported (a guiding error message exists).
5. **API 422 messages** reuse texts with `--by`/`--reason` (CLI flavor) — revisit when the transition UI evolves.
6. The spec's v1.5 (pluggable LLM): `cerne compile` (wiki) and `cerne ask` — not started, by design of the cut.

## Inventory (summary)

- `cmd/cerne/` — entrypoint. `internal/`: `domain` (FSMs, gates, events), `store` (entities, layout, events, IDs), `gitback` (go-git), `project` (config, lock, paths), `app` (transition/artifact core), `materialize`, `importer` (+hubtree testdata), `lint`, `index` (FTS5), `server` (API + static), `cli` (12 commands), `doccheck`, `slug`. `ui/` — Vite + React 19 + TS: kanban, feature page, DiagramPanel (lazy Excalidraw), typed api.ts; `dist/` committed. `e2e/` — golden path. `.gitlab-ci.yml`, `.gitattributes`.
- Direct Go dependencies: cobra, go-git/v5, BurntSushi/toml, modernc.org/sqlite. UI: react, react-dom, react-markdown, @excalidraw/excalidraw.

## Suggested next steps

1. Immediate dogfooding: `cerne import` of the cerne repo itself + the hub into a real backend (GitLab `rockt/hub-docs`), and start managing cerne's cases through cerne.
2. Case 0002: historical entity registry (open item 1) — fully closes the Bills criterion.
3. Case 0003: `cerne compile`/`ask` (v1.5, pluggable LLM).
4. Release pipeline (goreleaser or GitLab CI): win/mac/linux binaries + UI built in CI.
