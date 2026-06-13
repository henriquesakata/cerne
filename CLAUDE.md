# cerne — living documentation for AI-assisted projects

**cerne** is a standalone, open-source-in-potential CLI + local web UI (single Go binary) that manages the living documentation of a software project: it separates what is **current** from what is **history**, materializes only the current state into a gitignored `.docs/` folder that AI agents read, and keeps the lifecycle of features/decisions/references as **structured data** (never prose).

- **Why it exists, full design, decisions:** [docs/process/cases/0001_founding-spec/spec.md](docs/process/cases/0001_founding-spec/spec.md)
- **What shipped + acceptance numbers + debts + next steps:** [docs/process/cases/0001_founding-spec/result.md](docs/process/cases/0001_founding-spec/result.md)
- **Status:** v1 shipped (tags `v0.1.0`, `v0.2.0`). First real client: the Rockt Hub, dogfooding against `git@gitlab.com:rockt/hub-docs.git`.

## Toolchain (Windows dev box)

- **Go 1.26.4** lives at `C:\development\tools\go\bin` (on the user PATH for new shells). In the Bash tool, prefix commands: `export PATH="/c/development/tools/go/bin:$PATH"`.
- **Node v24** for the UI (`ui/`).
- Module path: `gitlab.com/rockt/cerne`.

## Build / test / run

```bash
go build -o cerne.exe ./cmd/cerne     # build the binary (UI is embedded via go:embed)
go vet ./... && go test ./... -count=1 # full suite — currently 235 tests, must stay green
go test ./internal/<pkg>/ -run TestX -v # focused
cd ui && npm ci && npm run build       # rebuild the React UI; commit ui/dist (embedded). npm ci (not install) = exact lockfile → reproducible dist
```

- CI: `.gitlab-ci.yml` runs `go vet` + `go test` on `golang:1.26`.
- `go install ./cmd/cerne` installs `cerne` to `C:\Users\Henrique\go\bin` (on PATH).
- Try the UI: `cerne ui` (serves `localhost:7717`).

## Architecture (one binary, four layers)

Backend git repo = **source of truth** (entities as JSON + content as markdown + events as NDJSON). SQLite (`<cache>/index.db`) is a **derived, rebuildable** FTS5 cache. `.docs/` is a **projection** of current state; `docs.lock` (committed in the user's project) pins the backend snapshot.

| Package | Role |
|---|---|
| `internal/domain` | Lifecycle FSMs (feature/decision/reference), gates, events. Pure, no I/O. |
| `internal/store` | Entity JSON persistence, backend layout, event log, `NextID`. |
| `internal/gitback` | go-git ops: clone/commit/push/pull, SSH auth (key-file fallback + known_hosts algo pinning). |
| `internal/project` | `.cernerc` (TOML), `docs.lock` (JSON), cache/index paths. |
| `internal/app` | Shared transition/artifact core (CLI + server call it). `CommitAndSnapshot` = commit+push+lock. |
| `internal/materialize` | `pull`: project current state into `.docs/`, generate `CONTEXT.md`. |
| `internal/importer` | `import`: scan an existing doc tree (hub conventions) → entities. |
| `internal/lint` | Deterministic staleness rules (ref-to-superseded, broken-link, orphan, missing-gate-doc). |
| `internal/index` | SQLite FTS5 over full history (incl. superseded). |
| `internal/server` | Local web API + embedded UI (`internal/cli/uicmd.go` serves it). |
| `internal/cli` | cobra commands: init, feature, status, pull, push, import, lint, search, ui. |
| `internal/doccheck`, `internal/slug` | Shared helpers (stub detection, slugify with diacritics). |
| `ui/` | Vite + React 19 + TS: kanban, feature page, Excalidraw (lazy). `dist/` committed + embedded. |
| `e2e/` | Black-box golden-path test through `cli.Root()`. |

## Conventions

- **TDD, strictly**: write the failing test, run it red, implement minimally, run green, commit. One concern per commit.
- **Error style**: sentinel errors (`var ErrX = errors.New(...)`) wrapped with `%w`, matched via `errors.Is`. Table-driven tests, error paths pinned with `errors.Is`.
- **Language**: user-facing CLI/UI strings are **pt-BR**; all code, comments, identifiers, and these AI-facing docs are **English**.
- **Data vs UI**: lifecycle states are English strings in data (`candidate`, `approved`, `in_development`, `implemented`, `rejected`, `superseded`); pt-BR labels live only in the UI.
- **Commits**: Conventional Commits; end the body with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- **Minimal surface**: no speculative API; keep exported identifiers to what is used.
- **Dogfooding**: this repo follows the same methodology cerne formalizes — `spec.md` → `plan.md` → `result.md` per case under `docs/process/cases/NNNN_name/`.

## Next steps (from case 0001 result.md)

1. **Case 0002 — historical entity registry**: a way to declare "X existed and was absorbed by Y" when X predates the case convention (the Bills/Investments gap — lint can't flag mentions of an entity that was never recorded). Closes acceptance criterion 1 fully.
2. **Case 0003 — LLM layer (v1.5)**: `cerne compile` (recompile wiki pages) + `cerne ask` (Q&A over current state, dive into history). Deterministic core stays LLM-free; LLM is pluggable (BYO key or delegate to host agent).
3. **Release pipeline**: goreleaser or GitLab CI producing win/mac/linux binaries with the UI built in CI (so `ui/dist` need not be committed); stamp version via ldflags (currently hardcoded `0.0.1-dev`).
4. **Lint coverage**: wiki-links (`[[note]]`, Obsidian style) are not parsed yet — only inline `[x](y)` links.

When starting a new case, use the brainstorming → writing-plans → (subagent-driven-development) → result.md flow.
