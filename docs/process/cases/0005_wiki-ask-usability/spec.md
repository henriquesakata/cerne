# Case 0005 — Usabilidade da wiki e do `ask` (follow-up do 0003)

tags: #spec #cerne #stub #llm #wiki #compile #ask

**Status:** Draft (stub — precisa de rodada de brainstorming para fechar decisões)
**Tipo:** Correção de usabilidade da camada LLM (case 0003). É o "case 0003.1" antecipado no [result.md do 0003](../0003_llm-compile-ask/result.md).
**Origem:** Revisão do case 0003 (2026-06-13). A camada está **fiel à spec e test-green**, mas o dogfood revelou que ela **não entrega valor num projeto maduro**. Os 3 achados abaixo foram **confirmados em código** durante a revisão.

---

## Problema

`cerne compile`/`ask` (case 0003) passam em todos os testes, mas três limitações — todas verificadas no código — impedem o uso real:

**A. `compile` ignora features `implemented`.** [`internal/compile/compile.go:40-43`](../../../internal/compile/compile.go) define `liveFeatureStates` = candidate/approved/in_development; usado nas fontes da página por módulo (linha 78) e da página `project` (linha 98). Num projeto maduro (hub: **285/308 implemented**) a wiki quase não tem fontes — ela **não sintetiza o sistema já entregue**, que é exatamente o que uma wiki deveria capturar. (Contraste: lint e materialize excluem `implemented` do `.docs/` corretamente, porque ali o objetivo é "trabalho em curso"; a wiki tem objetivo oposto — explicar o que existe hoje.)

**B. Página `project` sem chunking.** [`compile.go:143-152`](../../../internal/compile/compile.go) (`BuildPagePrompt`) concatena o **conteúdo completo** de cada fonte. Na página transversal do hub (45 decisões ativas + 66 referências vigentes) isso gera um prompt de **~33k linhas** — grande demais para uma chamada LLM.

**C. `ask` sem extração de keywords.** [`internal/ask/ask.go:32`](../../../internal/ask/ask.go) passa a pergunta inteira como query FTS literal (`index.Search(dbPath, query, limit)`). Uma pergunta em linguagem natural vira um match FTS literal e não casa → **recall ~zero**; só queries por palavra-chave recuperam algo.

Bônus de dívida (case 0003): o caminho síncrono `Provider.Complete` foi removido do runtime (compile/ask são delegação-only; o `Fake` só existe em teste). A interface BYO-key está **testada só com fake, nunca exercida de verdade** — relevante porque testar as correções abaixo de forma determinística pode pedir esse caminho de volta.

---

## Objetivo e critérios de sucesso (rascunho)

`compile`/`ask` **úteis num projeto real/maduro**:
- A wiki sintetiza o **sistema vigente entregue** (inclui `implemented`), não só o trabalho em curso.
- `compile` lida com agregações grandes (página transversal) sem estourar o tamanho de prompt.
- `ask` responde a **perguntas em linguagem natural** com recall razoável.

---

## Direção a explorar (alternativas — decidir no brainstorming)

**A — incluir `implemented` nas fontes da wiki:** introduzir um conjunto `wikiSourceStates` (live + implemented) distinto do `liveFeatureStates` de lint/materialize. Cuidado: combina com o problema B (mais fontes ⇒ prompt maior).

**B — chunking/sumarização:**
1. **Split da página `project`**: separar em página de decisões + página de referências (cada uma já menor).
2. **Map-reduce / pré-sumário por entidade**: compilar um resumo curto por entidade primeiro, depois sintetizar a página a partir dos resumos (hierárquico). A wiki por módulo já "chunka" features por módulo — falta o mesmo para o transversal.
3. **Cap + paginação**: limite de tamanho por prompt com continuação.

**C — extração de keywords no `ask`:**
1. **Determinística**: remoção de stopwords (pt-BR/en) + extração de termos salientes, OR-join no FTS.
2. **Delegada ao agente**: `ask` em duas fases (agente extrai keywords / reformula a query) — coerente com o modelo de delegação do 0003.
3. **Expansão de query FTS** (prefix/OR) sobre os termos.

---

## Questões em aberto

1. Incluir `implemented` agrava B em quanto? A e B precisam ser resolvidos juntos?
2. Estratégia de chunking: split de páginas (simples) vs map-reduce (melhor síntese, mais chamadas)?
3. Extração de keywords determinística vs delegada — e o `ask` vira duas fases?
4. Para testar as correções de forma determinística, vale **ressuscitar o caminho síncrono `Provider.Complete`** (com o `Fake`)? Isso reabre a dívida do BYO-key — talvez este case também feche o "pluggable" de ponta a ponta.
5. A wiki deve ter páginas temáticas transversais (deferred do 0003) ou só módulos + project?

---

## Próximos passos

1. **Brainstorming** para fechar A/B/C e decidir se o caminho síncrono volta.
2. `plan.md` (TDD): provavelmente `wikiSourceStates` (A) → chunking do compile (B) → keyword extraction no ask (C) → re-dogfood no hub.
3. `result.md`.
