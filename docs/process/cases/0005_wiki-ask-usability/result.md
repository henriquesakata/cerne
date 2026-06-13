# Resultado — Case 0005 (usabilidade wiki/ask)

**Status:** Done (2026-06-13) para o escopo A/B/C. Suíte: 307 testes verdes, `go vet` limpo. Uma descoberta do dogfood foi extraída para o [case 0006](../0006_wiki-page-granularity/spec.md).

## O que foi entregue

Os três gaps de usabilidade da camada LLM (case 0003) confirmados na revisão:

- **A — wiki inclui `implemented`.** `internal/compile/compile.go`: `liveFeatureStates` → **`wikiSourceStates`** (+`implemented`). Local ao pacote `compile`; `materialize`/`lint` intactos (diff vazio confirmado). Commit `f85e535`.
- **B — excerpt limitado por fonte.** `BuildPagePrompt` agora limita cada fonte a `maxSourceExcerptChars = 800` (corte em fronteira de runa/linha + marcador). `InputHash`/dirty-tracking **continuam sobre o conteúdo integral** (asserção no teste). Commit `049f98a`.
- **C — extração de keywords no `ask`.** `internal/ask/ask.go`: `extractKeywords()` determinística (stopwords pt-BR/en, termos ≥3 runas, OR-join; fallback à query original). Commit `d45cb7f`.

## Verificação no corpus real do hub

Clone bare local do `hub-docs` (308 features), `cerne compile --force`:

| | Antes (0003) | Depois (0005) |
|---|---|---|
| Prompt da página `project` | ~33k linhas | **7,5k linhas (-77%)**, cada fonte limitada |
| Páginas por módulo | quase vazias (implemented excluído) | finance 895 / identity 526 / marketplace 280 linhas — **úteis** |
| `ask` pergunta NL ("por que bills/investments absorvidos…") | recall ~zero (query literal) | **recall alto**: F-0021 (Finance Consolidation), F-0025, F-0123, R-0003, D-0008 |

## Descoberta → case 0006 (honestidade de escopo)

O dogfood respondeu, na prática, a pergunta aberta da spec ("incluir implemented agrava B?"): **sim**, porque o importador do hub não atribui módulo aos cases raiz (`docs/process/cases/`). Resultado: a página `project` reúne **346 fontes** (44 decisões + 66 referências + 236 features sem módulo, 217 implemented). Está limitada (não estoura mais), mas grande demais para uma síntese boa.

Isso é um problema **distinto** de A/B/C — granularidade de página + como entidades sem módulo mapeiam para a wiki — e foi extraído para o **case 0006** (split de decisões/referências + tratamento de features sem módulo). As páginas por módulo e o `ask` **já são usáveis**; o overview transversal espera o 0006.

## Inventário

- **Código:** `internal/compile/compile.go` (wikiSourceStates, excerpt, maxSourceExcerptChars), `internal/ask/ask.go` (stopwords, extractKeywords).
- **Testes:** `internal/compile/compile_test.go`, `internal/ask/ask_test.go` (+6 testes; 301→307).
- **Commits:** `f85e535`, `049f98a`, `d45cb7f` + docs deste case.

## Follow-ups

- **Case 0006** (aberto): granularidade da wiki para entidades sem módulo / split de páginas transversais.
- Dívida 0003: caminho síncrono `Provider.Complete` / BYO-key segue exercido só pelo `Fake` em teste.
