# Case 0003 — Resultado

tags: #result #cerne #llm #wiki #compile #ask

**Status da spec:** Implementado (CLI, ambos os comandos, modo delegação) — suíte verde; **dogfood real pendente** (1 passo manual, ver abaixo).
**Suíte:** 257 testes verdes em 20 pacotes · `go vet` limpo · `go build ./...` ok.
**Spec/plan:** [spec.md](spec.md) · [plan.md](plan.md). Brainstorming + plano em `8ff0120`.

---

## O que foi entregue

Os 7 milestones do [plan.md](plan.md), em TDD estrito, um commit por preocupação:

| Milestone | Entrega | Commit |
|---|---|---|
| M1 | `internal/llm`: interface `Provider`, `Fake` determinístico, `ModeDelegation`/`ResolveMode`; bloco `[llm] mode` no `.cernerc` (segredo nunca aqui) | `56edf4d` |
| M2 | `internal/wiki`: `Source`, `InputHash` (ordem-independente, sha256 de `id\|state\|contentHash`), `Provenance` render/parse | `d3b007a` |
| M3 | `internal/compile`: `Pages` (página por módulo de features vivas + `project` transversal; só `project` quando sem módulos), `InputHash`/`Dirty`, `BuildPagePrompt` | `0f5ad69` |
| M4 | `cerne compile` (delegação: prepare → `--ingest`), `--scope`/`--force`, páginas vazias puladas; `content/wiki` no layout | `62b7420` |
| M5 | `materialize`: projeta `content/wiki/**` → `.docs/wiki/**` (artefato versionado, project-wide) | `2ffd2b7` |
| M6 | `internal/ask` + `cerne ask`: recuperação vigente-primeiro (FTS + wiki dos hits), marcada por estado, `--history`; delegação imprime o prompt | `3d90341` |
| M7 | `lint`: regra `wiki-source-unknown` + páginas de wiki no escopo de `ref-to-superseded`/`broken-link` | `ed13542` |

## Aceitação (critérios da spec → testes)

1. **Núcleo intacto sem `[llm]`** ✅ — toda a suíte do v1 segue verde; `[llm]` é opcional, default `delegation`.
2. **`compile` incremental** ✅ — `TestCompileDelegationFlow`: prepara só páginas sujas, `--ingest` carimba+commita, re-rodar ⇒ "Nada a recompilar"; `TestCompileForceRecompiles` cobre `--force`.
3. **Dirty exato por input-hash** ✅ — `internal/compile.TestDirtyTracking`: editar 1 feature suja só o módulo dela.
4. **`ask` monta prompt marcado, `--history` opt-in** ✅ — `internal/ask` + `TestAskDelegationPrintsPrompt`: default exclui terminais; `--history` inclui marcados.
5. **`ask` nunca trata substituído como vigente** ✅ — `TestRetrieveHistoryIncludesTerminalTagged`: o estado viaja em todo `Snippet`.
6. **lint da wiki** ✅ — `TestLintWikiRules`: `sources` inexistente ⇒ `wiki-source-unknown`; menção a entidade substituída na página ⇒ `ref-to-superseded`.

**Pendente (1 passo manual, não rodável nesta sessão):** dogfood real — `cerne import` do próprio repo cerne (ou do hub) num backend, `cerne compile` (este agente processa os prompts de `.docs/wiki/.pending/`) e `cerne ask`, anexando a wiki gerada e a resposta. Os caminhos determinísticos (page-set, dirty, retrieval, lint) já estão cobertos por teste; falta só ver a síntese real.

## Decisões / desvios em relação ao plano

- **Sem caminho síncrono (`Provider.Complete`) no runtime.** O plano previa um caminho "fake/byo" síncrono no `compile`/`ask`; como o único modo de runtime é delegação e o BYO-key é deferred, implementá-lo seria **dead code** (viola superfície mínima). `compile`/`ask` são **delegação-only**; o `Provider`+`Fake` ficam como seam testado (M1) para o BYO-key futuro. Ajuste consciente vs. spec.
- **Página `project.md` transversal** — `Decision`/`Reference` não têm campo `Module`, então decisões ativas + referências vigentes (e features sem módulo) vão numa página `project`. Correção feita na spec durante o planejamento.
- **Wiki versionada no backend** (`content/wiki/`) e projetada por `pull` em `.docs/wiki/` — síntese cara é compartilhada pela equipe, não recompilada por máquina.

## Pendências e dívidas

1. **Dogfood real** (acima) — único passo que falta para "ver" a camada funcionando ponta-a-ponta.
2. **Caminho síncrono + BYO-key**: a interface está pronta; falta a impl HTTP (chave por env) e o caminho síncrono de `compile`/`ask`. É o próximo recorte natural.
3. **Recuperação do `ask`**: hoje junta wiki por módulo dos hits + FTS; ranking de relevância de páginas de wiki (em vez do join por módulo) é refinamento.
4. **Deferred da spec, intactos**: UI (painel wiki + caixa ask), páginas temáticas transversais, dois-passos no `ask`.
5. **`pull` limpa `.docs/wiki/.pending/`** (RemoveAll do `.docs`) — se um `pull` ocorrer no meio de um ciclo compile, os prompts pendentes somem; basta re-rodar `compile`. Aceitável; anotado.

## Inventário (resumo)

- **Novos pacotes**: `internal/llm`, `internal/wiki`, `internal/compile`, `internal/ask` (+ testes).
- **Novos CLI**: `internal/cli/{compilecmd,askcmd}.go` (+ testes).
- **Tocados**: `internal/project/config.go` (`[llm]`), `internal/store/layout.go` (`content/wiki`), `internal/cli/root.go`, `internal/materialize/materialize.go` (projeção wiki), `internal/lint/lint.go` (regras de wiki).
- Sem dependências novas.

## Dogfood real no hub (2026-06-13)

Mesmo backend local (clone bare do `hub-docs`, 308 features), via `cerne compile`/`ask`.

- **compile (delegação) ✅:** `cerne compile` gerou prompts para as páginas com features vivas. Sintetizei `modules/finance.md` (1 fonte viva, `F-0268`); `compile --ingest --scope finance` carimbou a proveniência (`sources: [F-0268]`, `input_hash`), commitou; `pull` materializou em `.docs/wiki/`. Re-rodar → "Nada a recompilar" (dirty-tracking por input-hash confirmado).
- **ask ✅ (com keyword):** `cerne ask "cifragem"` recupera a página `wiki:modules/finance` (current) + features, todas marcadas por estado. Integração wiki + tagging confirmados.

**Achados (viram follow-up — ver pendências):**
1. **compile exclui `implemented`** (usa `liveFeatureStates` = candidate/approved/in_development). Num projeto maduro (285/308 implemented) só `finance` e `identity` tiveram página — a wiki não sintetiza o sistema vigente já entregue. Provável correção: incluir `implemented` nas fontes do compile (divergindo de lint/materialize).
2. **Página `project` impraticável:** o prompt embute o conteúdo de 45 ADRs + 66 refs → ~33k linhas, grande demais para uma chamada LLM. Precisa chunking/sumarização, não embutir tudo.
3. **`ask` com pergunta em linguagem natural: recall ~zero.** A frase inteira vira query FTS literal e não casa; só queries por palavra-chave recuperam. Precisa extração de keywords (ou retriever melhor) antes do `ask` ser útil para perguntas reais.

## Próximos passos sugeridos

1. **Corrigir os 3 achados do dogfood** (acima) — em especial incluir `implemented` no compile e a extração de keywords no `ask`; provavelmente um case 0003.1 antes de declarar a camada útil.
2. Case seguinte: BYO-key (provider HTTP, chave por env) + caminho síncrono — fecha o "pluggable" de ponta a ponta.
3. UI da wiki/ask (follow-up do escopo A).
