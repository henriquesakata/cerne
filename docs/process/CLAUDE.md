# Cases — cerne

Index of development cases. Each case folder contains `spec.md` (intent), `plan.md` (implementation plan) and `result.md` (what actually shipped). Numbering is sequential in development order.

| Case | Title | Status |
|---|---|---|
| [0001](cases/0001_founding-spec/spec.md) | Founding spec — product, architecture and v1 cut | Done (v0.2.0) |
| [0002](cases/0002_historical-entity-registry/spec.md) | Historical entity registry (Bills/Investments gap) | Implemented (gap closed; dedup → 0004) |
| [0003](cases/0003_llm-compile-ask/spec.md) | LLM layer — `compile` + `ask` (v1.5) | Implemented (usable-on-mature-project fixes → 0005) |
| [0004](cases/0004_historical-registry-idempotency/spec.md) | Historical registry idempotency (dedup) | Done |
| [0005](cases/0005_wiki-ask-usability/spec.md) | Wiki + `ask` usability (0003 follow-up) | Done (A/B/C; granularity → 0006) |
| [0006](cases/0006_wiki-page-granularity/spec.md) | Wiki page granularity for module-less entities | Done (module inference → 0007) |
| [0007](cases/0007_module-inference/spec.md) | Module inference/assignment (`classify` + import) | Done |
| [0008](cases/0008_cache-auto-recreate/spec.md) | Cache clone auto-recreate (no init dead-end) | Done |
| [0009](cases/0009_byo-key-provider/spec.md) | BYO-key LLM provider (`compile`/`ask` standalone) | Done |
| [0010](cases/0010_release-pipeline/spec.md) | Release pipeline (ldflags version, cross-compile, GitLab CI) | Done |
| [0011](cases/0011_ui-read-pages/spec.md) | UI nav shell + read pages (decisions, references, search, lint, history) | Done |
| [0012](cases/0012_feature-authoring-ui/spec.md) | Feature authoring & management in the UI (create/edit/"remove") | Done (stacked on 0011) |
| [0013](cases/0013_ui-open-browser/spec.md) | `cerne ui` opens the browser (default; `--no-browser`) | Done |
| [0014](cases/0014_claude-cli-provider/spec.md) | `claude-cli` provider (subscription) + `cerne spec draft` | Done |
| [0015](cases/0015_decision-authoring/spec.md) | Native decision authoring (`decision new`/`substituir`) + `feature supersede`→`substituir` rename | Implemented — suíte verde; aguardando revisão |
| [0016](cases/0016_sequence-diagram-draft/spec.md) | `cerne spec diagram` (Mermaid via assinatura) + render/edição na UI | Done |
| [0017](cases/0017_wiki-on-web/spec.md) | Wiki na web — perguntar (`/api/ask` via provider) + navegar a síntese | Done |
| [0018](cases/0018_import-classify-backfill/spec.md) | `import` auto-classifica módulos (sem subsistema órfão) | Done |
| [0019](cases/0019_llm-mode-in-ui/spec.md) | Trocar o modo de IA pela UI (`/api/config/llm` + seletor) | Done |
| [0020](cases/0020_external-references/spec.md) | Referências externas (marca + conteúdo indexado + backlink `informs`) | Done |
| [0021](cases/0021_entity-graph/spec.md) | Grafo explicativo de entidades (arestas tipadas+motivo, React Flow, layout em camadas) | Done (A/B/C; explicação LLM → follow-up) |
| [0022](cases/0022_serve-multiproject/spec.md) | `cerne serve` multi-projeto (fundação de integração com hosts; ex.: Studio) | Done (lado cerne da F1; hub → case em apps/studio) |
| [0023](cases/0023_push-net-new-entity-docs/spec.md) | `push` aceita plan/result net-new sob feature rastreada (desbloqueia fluxo cerne-native) | Done |
| [0024](cases/0024_serve-init-empty-backend/spec.md) | `serve` inicializa layout ao registrar backend vazio (provisão sob demanda do host) | Done |

Conventions (inherited from the methodology cerne itself formalizes):

- `spec.md` before any code; `plan.md` before implementation; `result.md` before merge.
- Prose in pt-BR; identifiers, commands and code in English.
- When cerne v1 exists, this tree gets imported via `cerne import` and managed by cerne itself (dogfooding).
