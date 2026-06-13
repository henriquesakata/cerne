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

Conventions (inherited from the methodology cerne itself formalizes):

- `spec.md` before any code; `plan.md` before implementation; `result.md` before merge.
- Prose in pt-BR; identifiers, commands and code in English.
- When cerne v1 exists, this tree gets imported via `cerne import` and managed by cerne itself (dogfooding).
