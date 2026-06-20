# Case 0038 — Resultado: template de ADR (dectmpl)

tags: #result #cerne #cli #dectmpl #docs

**Status:** Entregue (2026-06-20) — código verde; referências registradas no backend.

## O que entregou

- **`internal/dectmpl/dectmpl.go`** — template canônico de ADR: 5 seções em ordem (`Contexto`, `Decisão`, `Consequências`, `Alternativas consideradas`, `Implementação e relações`), `Body` (lido como stub pelo `doccheck`) e `Stub(title)`. Reusa o marcador `[ESCLARECER:` da spec; sem seção de status (ciclo é dado do cerne).
- **`internal/cli/decisioncmd.go`** — `cerne decision draft <id>`: monta o prompt (5 seções + regras: decisão imperativa, consequências negativas obrigatórias, alternativas com porquê, marcador de ambiguidade, right-sizing). A mensagem do `decision new` aponta o draft.
- **`internal/app/decision.go`** — `writeDecisionStub` agora materializa o template no `cerne decision new`.
- **`internal/doccheck/doccheck.go`** — `stubPrefixes` ganha `"> Decisão pendente"` (corrige lacuna do stub legado).
- **`internal/skills/cerne-authoring/SKILL.md`** — seção "ADR structure (the template cerne scaffolds)" + seção Decisions atualizada.

## Como foi embasado

Análise dos **46 ADRs do hub** (Nygard + MADR + custom; lacunas: negativas ausentes ~20%, alternativas finas, sem backlinks de supersessão) + pesquisa em fontes confiáveis, registradas como referências `--informs` esta feature: Michael Nygard (Documenting Architecture Decisions, 2011), MADR, ThoughtWorks Technology Radar (Lightweight ADRs — Adopt), o overview adr.github.io, e a skill create-adr (TLC: ADR vs RFC, título-como-decisão, imutabilidade/supersede, concisão 200–500 palavras). A skill tlc-spec-driven (já R-0014) também informa pela fase Design.

## Números de aceitação

- Suíte: **563 passed em 26 pacotes** (`go test ./... -count=1`), `go vet ./...` limpo.
- Critérios 1–5 cobertos por testes (`dectmpl_test.go`, `decisioncmd_test.go`, e2e `TestDecisionGoldenPath` atualizado). Critério 6: referências registradas no backend.

## Débitos e próximos passos

- `cerne lint` não valida completude das seções do ADR — follow-up.
- eADR / categorias do MADR (projetos grandes) fora de escopo.
- Considerar um ADR de demonstração no backend (dogfood do template).
