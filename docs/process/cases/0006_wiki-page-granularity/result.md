# Resultado — Case 0006 (granularidade de wiki / entidades sem módulo)

**Status:** Done (2026-06-13). Suíte: 316 testes verdes, `go vet` limpo.

## O que foi entregue

A wiki transversal deixou de ser um despejo único: decisões e referências viraram páginas próprias e limitadas, e features sem módulo deixam de inflar a wiki num projeto com módulos (cobertas por `CONTEXT.md`/`search`, com nota de contagem).

- **Split** (`internal/compile/compile.go`, `Pages()`): page-set agora é per-module + `decisions` (só se há decisão ativa) + `references` (só se há referência vigente) + `project` **apenas quando o projeto não declara módulos** (fallback pequeno com todas as features). Em projeto com módulos, features sem módulo entram em **nenhuma** página. Ordem determinística: módulos → decisions → references → project. Commit `fe910f4`.
- **Transparência** (`internal/compile/compile.go` `ModulelessSkipped` + `internal/cli/compilecmd.go`): a fase de preparação imprime "N feature(s) sem módulo não entram na wiki (veja CONTEXT.md / cerne search); atribua módulos para sintetizá-las." Commit `dd9d324`.
- **`ask`** (`internal/ask/ask.go` `Retrieve`): lê `decisions`/`references` quando há hit do tipo, além de `project` + páginas de módulo. Commit `71a0b8c`.
- **Lint** já validava `content/wiki/**` genericamente — sem mudança de regra, só teste pinando que `decisions.md` com source inexistente é flagrado. Commit `9e982c9`.
- `materialize.copyWiki` projeta `content/wiki/**` → `.docs/wiki/**` genericamente: as novas páginas fluem pelo `pull` sem mudança.

## Verificação no corpus real do hub

Clone bare local do `hub-docs`, `cerne compile --force`:

| Página | Antes (0005) | Depois (0006) |
|---|---|---|
| `project` (transversal) | 1 página, **7560 linhas / 346 fontes** | **eliminada** (projeto tem módulos) |
| `decisions` | — (dentro do project) | **1067 linhas / 44 fontes** |
| `references` | — (dentro do project) | **1591 linhas / 66 fontes** |
| módulos (finance/identity/marketplace) | 895 / 526 / 280 | inalteradas |
| features sem módulo (236) | no project | **nota impressa**; cobertas por CONTEXT.md/search |

Maior página caiu de 7560 → 1591 linhas. Toda página gerada é agora coerente e limitada.

## Decisões (ver spec)

A wiki sintetiza agrupamentos coerentes (módulos) + camadas transversais (decisões/referências); a cobertura completa é papel do `CONTEXT.md` + `search`. Sintetizar 236 cases raiz desconexos produziria ruído — então não se faz, e o número é reportado. Inferência de módulo (case-name→módulo) ficou para o **case 0007** (é modelagem/heurística, não granularidade).

## Inventário

- **Código:** `internal/compile/compile.go` (page-set split, `ModulelessSkipped`), `internal/cli/compilecmd.go` (nota), `internal/ask/ask.go` (retrieve decisions/references).
- **Testes:** `internal/compile/*`, `internal/cli/*`, `internal/ask/*`, `internal/lint/*` (+9; 307→316).
- **Commits:** `fe910f4`, `dd9d324`, `71a0b8c`, `9e982c9` + docs deste case.

## Follow-ups

- **Case 0007** (sugerido): inferência/atribuição de módulo para cases raiz no `import` — traria as 236 features sem módulo para as páginas de módulo certas e enriqueceria a wiki do hub.
- Dívida 0005/0003: caminho síncrono `Provider.Complete` / BYO-key segue exercido só pelo `Fake`.
