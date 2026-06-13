# Cases — cerne

Index of development cases. Each case folder contains `spec.md` (intent), `plan.md` (implementation plan) and `result.md` (what actually shipped). Numbering is sequential in development order.

| Case | Title | Status |
|---|---|---|
| [0001](cases/0001_founding-spec/spec.md) | Founding spec — product, architecture and v1 cut | Done (v0.2.0) |
| [0002](cases/0002_historical-entity-registry/spec.md) | Historical entity registry (Bills/Investments gap) | Draft (stub) |
| [0003](cases/0003_llm-compile-ask/spec.md) | LLM layer — `compile` + `ask` (v1.5) | Draft (spec) |

Conventions (inherited from the methodology cerne itself formalizes):

- `spec.md` before any code; `plan.md` before implementation; `result.md` before merge.
- Prose in pt-BR; identifiers, commands and code in English.
- When cerne v1 exists, this tree gets imported via `cerne import` and managed by cerne itself (dogfooding).
