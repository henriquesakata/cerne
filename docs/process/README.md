# Development cases (frozen history)

This tree is cerne's original case record: cases 0001–0040, plus `_native/0044`, written while cerne was bootstrapping itself. Each case folder holds `spec.md` (intent), `plan.md` (implementation plan) and `result.md` (what shipped).

- **It is frozen.** Nothing here is updated, and the *Status when closed* column records each case as it stood when it closed, not today. For the current state of any feature, ask cerne: `cerne status`, `cerne brief`, `cerne search "<q>"`. Since about case 0035, work is authored natively in the cerne backend (`cerne feature new`), and a hook (`.claude/hooks/no-new-case-dir.sh`) blocks creating new case folders.
- **It is in Brazilian Portuguese**, the project's working language at the time. Only the founding case is translated: [0001 spec](cases/0001_founding-spec/spec.md) and [0001 result](cases/0001_founding-spec/result.md). Its plan stays in Portuguese.
- **Numbering diverged from backend ids** around case 0035: the backend feature titled "Case 0035" is a different case from the folder `0035_register-task-at-start`. Cases 0035 and 0036 were never registered in the backend.

| Case | Title | Status when closed |
|---|---|---|
| [0001](cases/0001_founding-spec/spec.md) | Founding spec: product, architecture and v1 cut | Done (v0.2.0) |
| [0002](cases/0002_historical-entity-registry/spec.md) | Historical entity registry (Bills/Investments gap) | Implemented (gap closed; dedup → 0004) |
| [0003](cases/0003_llm-compile-ask/spec.md) | LLM layer: `compile` + `ask` (v1.5) | Implemented (fixes for mature projects → 0005) |
| [0004](cases/0004_historical-registry-idempotency/spec.md) | Historical registry idempotency (dedup) | Done |
| [0005](cases/0005_wiki-ask-usability/spec.md) | Wiki + `ask` usability (0003 follow-up) | Done (A/B/C; granularity → 0006) |
| [0006](cases/0006_wiki-page-granularity/spec.md) | Wiki page granularity for module-less entities | Done (module inference → 0007) |
| [0007](cases/0007_module-inference/spec.md) | Module inference/assignment (`classify` + import) | Done |
| [0008](cases/0008_cache-auto-recreate/spec.md) | Cache clone auto-recreate (no init dead-end) | Done |
| [0009](cases/0009_byo-key-provider/spec.md) | BYO-key LLM provider (`compile`/`ask` standalone) | Done |
| [0010](cases/0010_release-pipeline/spec.md) | Release pipeline (ldflags version, cross-compile, GitLab CI) | Done |
| [0011](cases/0011_ui-read-pages/spec.md) | UI nav shell + read pages (decisions, references, search, lint, history) | Done |
| [0012](cases/0012_feature-authoring-ui/spec.md) | Feature authoring and management in the UI (create/edit/"remove") | Done (stacked on 0011) |
| [0013](cases/0013_ui-open-browser/spec.md) | `cerne ui` opens the browser (default; `--no-browser`) | Done |
| [0014](cases/0014_claude-cli-provider/spec.md) | `claude-cli` provider (subscription) + `cerne spec draft` | Done |
| [0015](cases/0015_decision-authoring/spec.md) | Native decision authoring (`decision new`/`substituir`) + `feature supersede` → `substituir` rename | Implemented: suite green, awaiting review |
| [0016](cases/0016_sequence-diagram-draft/spec.md) | `cerne spec diagram` (Mermaid via subscription) + render/edit in the UI | Done |
| [0017](cases/0017_wiki-on-web/spec.md) | Wiki on the web: ask (`/api/ask` via provider) + browse the synthesis | Done |
| [0018](cases/0018_import-classify-backfill/spec.md) | `import` auto-classifies modules (no orphan subsystem) | Done |
| [0019](cases/0019_llm-mode-in-ui/spec.md) | Switch the AI mode from the UI (`/api/config/llm` + selector) | Done |
| [0020](cases/0020_external-references/spec.md) | External references (marker + indexed content + `informs` backlink) | Done |
| [0021](cases/0021_entity-graph/spec.md) | Explanatory entity graph (typed edges with reasons, React Flow, layered layout) | Done (A/B/C; LLM explanation → follow-up) |
| [0022](cases/0022_serve-multiproject/spec.md) | Multi-project `cerne serve` (foundation for host integrations) | Done (cerne side) |
| [0023](cases/0023_push-net-new-entity-docs/spec.md) | `push` accepts net-new plan/result under a tracked feature (unblocks the cerne-native flow) | Done |
| [0024](cases/0024_serve-init-empty-backend/spec.md) | `serve` initializes the layout when registering an empty backend (on-demand host provisioning) | Done |
| [0025](cases/0025_ui-generate-diagram/spec.md) | "Generate with AI" button in the UI: sequence diagram (Mermaid → editable Excalidraw) | Done |
| [0026](cases/0026_diagram-grounded-in-code/spec.md) | Diagram generation grounded in the project's code and structure | Done |
| [0027](cases/0027_feature-epics/spec.md) | Epics: native parent/child between features (inferred epic, `part_of` edge, rollup, lint, UI) | Implemented: suite green, awaiting review |
| [0028](cases/0028_skill-distribution/spec.md) | Canonical `cerne-authoring` skill distributed by the tool (`cerne skill install` + embed) | Implemented: suite green, awaiting review |
| [0029](cases/0029_init-installs-skills/spec.md) | `cerne init` installs the canonical skill in new projects | Implemented: suite green, awaiting review |
| [0030](cases/0030_docs-lock-conflict-free/spec.md) | Conflict-free `docs.lock`: local manifest + merge driver for the snapshot | Implemented: suite green (506), real e2e |
| [0031](cases/0031_ui-live-refresh/spec.md) | Live UI: the screen refreshes itself (SSE + HEAD watcher + optional periodic pull) | Implemented: suite green (513), awaiting manual browser check |
| [0032](cases/0032_ui-multi-project/spec.md) | Multi-project `cerne ui`: host + join on a busy port, per-project routes, multiplexed SSE, global selector | Done: suite green (520), verified in the browser |
| [0033](cases/0033_unify-route-tables/spec.md) | Single route table reused by `newMux`/`uimulti`/`serve` | Implemented: suite green (523), refactor without behavior change |
| [0034](cases/0034_remove-dead-livehub/spec.md) | Remove the dead single-project live path | Implemented: suite green (518), dead code removal |
| [0035](cases/0035_register-task-at-start/spec.md) | Register the task at the start (`cerne-authoring` rule + `SessionStart` hook); candidate as the in-flight work inbox | Implemented: suite green (548), backend registration pending |
| [0036](cases/0036_stale-candidate-lint/spec.md) | `stale-candidate` lint: a candidate older than 30 days without a real spec | Implemented: suite green (548), backend registration pending |
| [0037](cases/0037_plan-template/spec.md) | Plan template provided by cerne | Delivered (2026-06-20) |
| [0038](cases/0038_adr-template/spec.md) | ADR (decision) template provided by cerne | Delivered (2026-06-20) |
| [0039](cases/0039_result-template/spec.md) | Result template provided by cerne | Delivered |
| [0040](cases/0040_runbook-entity/spec.md) | Runbook as a first-class entity | Delivered |
| [0044](_native/0044_agent-knowledge-notes/spec.md) | Note: shared agent knowledge as an entity (registered natively; lives in `_native/`) | Delivered |
