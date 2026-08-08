# cerne — living documentation for AI-assisted projects

**cerne** is a standalone, open-source-in-potential CLI + local web UI (single Go binary) that manages the living documentation of a software project: it separates what is **current** from what is **history**, materializes only the current state into a gitignored `.docs/` folder that AI agents read, and keeps the lifecycle of features/decisions/references as **structured data** (never prose).

- **Why it exists, full design, decisions:** [docs/process/cases/0001_founding-spec/spec.md](docs/process/cases/0001_founding-spec/spec.md)
- **What shipped + acceptance numbers + debts + next steps:** [docs/process/cases/0001_founding-spec/result.md](docs/process/cases/0001_founding-spec/result.md)
- **Status:** v1 shipped (tags `v0.1.0`, `v0.2.0`). First real client: the Rockt Hub, dogfooding against `git@gitlab.com:rockt/hub-docs.git`.

## Documentation source of truth: cerne (this repo dogfoods itself)

cerne is the **guardian and source of truth** for this project's documentation. The truth lives in the **`cerne-docs` backend** (`git@gitlab.com:rockt/cerne-docs.git`), pinned by the committed `docs.lock`; `.cernerc` wires it. Query and change docs through cerne — loose markdown is authoring input, not authority.

- **Read current state:** start at `.docs/CONTEXT.md` (generated: feature roster + states). The 21 founding cases are all `implemented`, so their prose is **history**, not materialized — reach it via `cerne search "<q>"`, `cerne ask "<q>"`, or the `docs/process/cases/` tree (now **frozen history** — see *Authoring*). For a token-lean view, prefer `cerne status --active` (only active work — hides the implemented backlog; F-0055), `cerne next` (one call: active features grouped by next action + `cerne lint` debts; F-0058), or `cerne brief` (the whole roster as `id  state  title`, no paths — the cheapest map for priming the project; supports `--json`; F-0064). `note list`, `search` and `ask` also take `--json`; `ask` adds `--max-sources`/`--max-snippet`/`--no-preamble` to trim its token scaffold (F-0064/F-0065).
- **State is data, never prose:** "what exists / in which state" → `cerne status` (incl. the **ÉPICOS** rollup). Lifecycle moves only via gated commands: `cerne feature new|approve|start|complete|reject|substituir` (also `decision new|substituir`, `retire`). **Epics are native:** a feature with children is an epic (role inferred, no flag) — link via `cerne feature new … --parent F-…` or `cerne feature reparent <id> <parent>|--clear`; it surfaces in `cerne status` + `CONTEXT.md` (Épico column / `## Épicos`) and `cerne lint` flags a live feature under a terminal epic, an epic completed with open children, or an epic still candidate/approved while a child is already in development (`epic-behind-children`). `import` doesn't carry `parent` yet, so set epics via the CLI/UI, not the case files.
- **External sources are provenance, not loose links:** when an external article / doc / spec informs a case, decision, or fix, register it — `cerne reference new "<title>" --url <url> --informs F-…,D-… [--file <summary> | stdin]`. It is marked external, its content is indexed (so `cerne ask` cites it), and `--informs` backlinks it to the entity it motivated. Don't leave a technical reference as a bare URL in prose — `cerne lint` flags a **decision/feature** doc citing an external URL with no backing reference as **`unregistered-source`** (F-0103/F-0107 — internal/local hosts and `.cernerc` `unregistered_source_ignore` are skipped), and `cerne hook install` wires a PostToolUse gate reminder so agents register sources without being asked.
- **Authoring (cerne-native, like the hub):** register a new case the **native** way — `cerne feature new` (mints the candidate when work starts, per case 0035), draft spec/plan/result, `cerne pull` to materialize the doc slots under `.docs/current/features/F-NNNN/`, fill each + `cerne push` → `cerne feature approve`/`start`/`complete`; commit `docs.lock`. ⚠️ **Never `cerne import` a new case:** the case-dir numbering (`docs/process/cases/NNNN_`) has diverged from backend ids (since ~F-0035), so `import` mints a **duplicate** `implemented` feature instead of matching the existing one (a dry-run lists 0035/0036/0044 as un-imported). `import` is only for the legacy bulk bootstrap. Edit **active** features via `.docs/current/` + `cerne push`; edit **implemented** cases (history, not in `.docs/`) directly in the `cerne-docs` clone. The `docs/process/cases/` files are drafting scratch — cerne does not pick them up automatically. As a backstop (F-0045), `cerne import` is now **guarded**: it skips a candidate whose normalized title already matches a backend feature, so a natively-registered case is never re-minted; and `docs/process/_native/` (sibling of `cases/`, outside the import scan) holds registered drafts — `0044_agent-knowledge-notes` lives there. The still-unregistered orphans `0035`/`0036` remain under `cases/` (a documented follow-up). The whole `docs/process/cases/` tree is **frozen history** — a `PreToolUse` hook (`.claude/hooks/no-new-case-dir.sh`, F-0046) blocks creating a new case-dir, so authoring stays cerne-native.
- **The authoring guide ships with cerne — and this repo consumes it (F-0046):** the canonical, project-agnostic `cerne-authoring` skill (`internal/skills/`) is installed into any project via `cerne skill install` → `.claude/skills/cerne-authoring/` (gitignored, tool-managed — never hand-edit; fix it upstream in `internal/skills/` and re-run install). This repo now dogfoods it like the hub: the installed copy is the canonical authoring guide here too.
- **End state:** `docs/` is kept during the transition; the goal is cerne as sole guardian, at which point the source tree becomes removable.

## Toolchain (Windows dev box)

- **Go 1.26.4** lives at `C:\development\tools\go\bin` (on the user PATH for new shells). In the Bash tool, prefix commands: `export PATH="/c/development/tools/go/bin:$PATH"`.
- **Node v24** for the UI (`ui/`).
- Module path: `gitlab.com/rockt/cerne`.

## Build / test / run

```bash
go build -ldflags="-s -w" -o cerne.exe ./cmd/cerne # build the binary (UI is embedded via go:embed)
go vet ./... && go test ./... -count=1 # full suite — currently 235 tests, must stay green
go test ./internal/<pkg>/ -run TestX -v # focused
cd ui && npm ci && npm run build       # rebuild the React UI; commit ui/dist (embedded). npm ci (not install) = exact lockfile → reproducible dist
```

- **`ui/dist` is `go:embed`-ed → the committed dist is what ships.** A UI change lands only if `ui/dist` is rebuilt *and* committed (F-0136 — F-0125/F-0126 shipped stale because only `ui/src` was merged). **Canonical build platform = CI Linux (`node:24`).** Do **not** commit a dist built on darwin/Windows — the vite output is not bit-identical across toolchains (hash-churn on locale chunks), so a foreign dist fails the drift gate. Build the baseline in a `node:24` Linux container (`docker run --platform linux/amd64 -v "$PWD":/app -w /app/ui node:24 sh -c "npm ci && npm run build"`) and commit that.
- CI: `.gitlab-ci.yml` runs `go vet` + `go test` on `golang:1.26`, plus **`ui:dist-drift`** (`node:24`, on `ui/**` changes): rebuilds the UI and fails if the committed `ui/dist` differs from a fresh build — the guard against stale embedded UI.
- **`-ldflags="-s -w"` on every binary build.** Linking the 48 MB binary (9.6 MB of it the embedded `ui/dist`) dominates the build: stripping the symbol table + DWARF takes the link from **11.1 s → 3.5 s** and the binary from **48 MB → 35 MB**, and `go build ./...` (what the pre-commit hook runs) from **17.4 s → 5.3 s**. Panic tracebacks stay symbolized — Go's runtime reads its own pclntab, not the symtab. The only loss is `delve`: drop the flags for a debugging build.
- `go install -ldflags="-s -w" ./cmd/cerne` installs `cerne` to `C:\Users\Henrique\go\bin` (on PATH).
- **RULE — reinstall the dogfood binary after changing cerne.** This repo runs its *own* `cerne` (the binary on PATH), so a behavior change (new command/entity/flag/lifecycle) is **invisible until you reinstall**: `go install -ldflags="-s -w" ./cmd/cerne`. Otherwise `cerne …` silently runs the previous build — e.g. `cerne note` does not exist until the F-0044 build is installed. Always reinstall before dogfooding the change you just made.
- Try the UI: `cerne ui` (serves `localhost:7717`).

## Architecture (one binary, four layers)

Backend git repo = **source of truth** (entities as JSON + content as markdown + events as NDJSON). SQLite (`<cache>/index.db`) is a **derived, rebuildable** FTS5 cache. `.docs/` is a **projection** of current state; `docs.lock` (committed in the user's project) pins the backend snapshot **only** — the materialized manifest lives in a gitignored `.docs/.manifest.json` sidecar, and a `cerne lock merge` git driver (wired via `.gitattributes`, registered by `init`/`pull`) keeps `docs.lock` conflict-free across parallel branches/worktrees (case 0030). With `pin_branch = "main"` in `.cernerc` (F-0102, **active in this repo**) `docs.lock` is written/committed only on main — feature branches track the live snapshot in the gitignored `.docs/.snapshot` sidecar and never touch the pin (so MRs can't conflict on it; the pin advances on the first cerne operation on main after merge — an unchanged `docs.lock` on a feature branch is correct, don't "fix" it). `cerne pull` 3-way **merges** unpushed `.docs/` edits instead of refusing — a disjoint remote advance is folded in while local edits are kept; a same-document conflict keeps yours and stashes the remote version under `.docs/.conflicts/` (`--force` discards local edits; F-0056/F-0077). Reconciliation is **per document** at the git layer: `cerne push` auto-rebases over advances on *other* documents (an advisory lock serializes the shared clone), and `cerne sync` = pull + push as one non-deadlocking forward path (F-0077). `cerne pull --okf` (or `okf = true` in `.cernerc`) emits the projection as an Open Knowledge Format–compatible bundle — frontmatter with the required `type` plus reserved/extra fields — default off (F-0054).

| Package | Role |
|---|---|
| `internal/domain` | Lifecycle FSMs (feature/decision/reference), gates, events. Pure, no I/O. |
| `internal/store` | Entity JSON persistence, backend layout, event log, `NextID`. |
| `internal/gitback` | go-git ops: clone/commit/push/pull, SSH auth (key-file fallback + known_hosts algo pinning). |
| `internal/project` | `.cernerc` (TOML), `docs.lock` (JSON), cache/index paths. |
| `internal/app` | Shared transition/artifact core (CLI + server call it). `CommitAndSnapshot` = commit+push+lock. |
| `internal/materialize` | `pull`: project current state into `.docs/`, generate `CONTEXT.md`. |
| `internal/importer` | `import`: scan an existing doc tree (hub conventions) → entities. |
| `internal/lint` | Deterministic staleness rules (ref-to-superseded, broken-link, orphan, missing-gate-doc, unregistered-source). |
| `internal/hooks` | Embeds + installs cerne's Claude Code hooks (`cerne hook install`): the provenance gate reminder + settings.json merge. |
| `internal/index` | SQLite FTS5 over full history (incl. superseded). |
| `internal/server` | Local web API + embedded UI (`internal/cli/uicmd.go` serves it). |
| `internal/cli` | cobra commands: init, feature, status, next, pull, push, import, lint, search, ui, lock (`lock merge` = the docs.lock git merge driver). |
| `internal/doccheck`, `internal/slug` | Shared helpers (stub detection, slugify with diacritics). |
| `ui/` | Vite + React 19 + TS: kanban, feature page, Excalidraw (lazy). `dist/` committed + embedded. |
| `e2e/` | Black-box golden-path test through `cli.Root()`. |

## Conventions

- **TDD, strictly**: write the failing test, run it red, implement minimally, run green, commit. One concern per commit.
- **Error style**: sentinel errors (`var ErrX = errors.New(...)`) wrapped with `%w`, matched via `errors.Is`. Table-driven tests, error paths pinned with `errors.Is`.
- **Language**: user-facing **CLI output** (messages, errors, cobra help) is **English** (F-0124); the **React UI** (`ui/`) strings stay **pt-BR**; all code, comments, identifiers, and these AI-facing docs are **English**. Exception: LLM **doc-authoring prompts** (spec/plan/result/decision/runbook draft builders, `ask`/`compile`) stay **pt-BR** on purpose — they set the language of the *generated documents*, not CLI chrome.
- **Data vs UI**: lifecycle states are English strings in data (`candidate`, `approved`, `in_development`, `implemented`, `rejected`, `superseded`); pt-BR labels live only in the UI.
- **Commits**: Conventional Commits; end the body with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- **Minimal surface**: no speculative API; keep exported identifiers to what is used.
- **Dogfooding**: this repo is itself a cerne project — its docs are managed by cerne (source of truth: the `cerne-docs` backend; see *Documentation source of truth* above). Cases are authored as `spec.md` → `plan.md` → `result.md`, then registered through the **native** flow (`cerne feature new` → `cerne push` → `approve`/`start`/`complete`) — **not** `cerne import` (see *Authoring* above for why).

## AI session conventions (insights follow-up)

Distilled from usage-insights friction (2026-07). Concrete contracts for recurring session shapes:

- **Observer / memory sessions**: every turn emit **exactly one** correctly-tagged output (`observation` vs `summary`, as asked) — visible, never empty, no meta-commentary about the tag format. Skip a checkpoint when nothing new happened since the last summary (no near-duplicate progress dumps).
- **Wiki / doc synthesis**: use the `/wiki-synth` skill — gather every cited cerne source, **verify each one's current state** via `cerne brief --json` / `cerne status --active` (never trust stale prose), then write **pt-BR** markdown to the session scratchpad. Split large `cerne ask` queries per-source to dodge the 30s timeout.
- **Feature delivery gate**: before `cerne feature complete` or opening an MR, `go vet ./... && go test ./... -count=1` must be green; if `ui/**` changed, rebuild + commit `ui/dist` (F-0136). A `git commit` PreToolUse hook (`.claude/hooks/go-precommit-build.sh`) runs `go build ./...` as an early tripwire — `git commit --no-verify` bypasses it.

## Roadmap and open work

This file does not track next steps — a hand-maintained roadmap is exactly the staleness cerne exists to kill (the founding spec's problem #3). Ask the guardian: `cerne status` for feature states, `cerne lint` for documentation debts, and the open cases under `docs/process/cases/`.

Wiki-links (F-0167): `cerne lint` and the entity graph resolve **id-form** wiki-links (`[[F-0123]]`, `[[N-0007]]`, alias `[[id|text]]`) — an unknown id is a `broken-link` finding; a resolvable one becomes a `links` edge and feeds the UI's Backlinks panel. Title-form `[[some title]]` is intentionally **not** a link (cut by design: zero false positives on TOML/prose — see the F-0167 spec).

When starting a new case, register it on the board first (`cerne feature new` — case 0035), then use the brainstorming → writing-plans → (subagent-driven-development) → result.md flow, pushing spec/plan/result to the tracked feature and advancing it via `cerne feature approve`/`start`/`complete`; commit the updated `docs.lock`. Register through the native flow, **not** `cerne import` (the case-dir numbering has diverged — see *Authoring*).
