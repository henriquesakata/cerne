# Case 0005 — Usabilidade da wiki e do `ask` (follow-up do 0003)

tags: #spec #cerne #llm #wiki #compile #ask

**Status:** Done — A/B/C implementados e verificados no corpus do hub (2026-06-13); ver [result.md](result.md). Descoberta no dogfood (página `project` com muitas features sem módulo) extraída para o **case 0006**.
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

## Decisões tomadas (2026-06-13)

| Fix | Decisão | Consequência |
|---|---|---|
| **A — implemented na wiki** | Conjunto **`wikiSourceStates`** local ao pacote `compile` = candidate/approved/in_development/**implemented** (sem terminais) | A wiki passa a sintetizar o sistema entregue; `materialize`/`lint` **inalterados** (seguem excluindo implemented na visão de trabalho-em-curso) |
| **B — chunking** | **Excerpt limitado por fonte** (`maxSourceExcerptChars = 800`, corte em fronteira de runa/linha + marcador `…(conteúdo truncado)`) no `BuildPagePrompt`. `input_hash` **continua sobre o conteúdo integral** | Prompt do `project` caiu de ~33k para ~7,5k linhas (-77%); páginas por módulo bem dimensionadas. **Split de páginas NÃO foi feito** (ver descoberta → 0006) |
| **C — keywords no `ask`** | **Determinística**: tokeniza (unicode), lowercase, descarta stopwords pt-BR/en + termos < 3 runas, dedup, **OR-join**; sem termo ⇒ usa a query original | Recall de pergunta NL passou de ~zero para alto; sem LLM, testável; sem 2 fases |

**Dívida do caminho síncrono / BYO-key:** mantida fora de escopo. Os testes usam o `Fake`; o caminho `Provider.Complete` segue exercido só por ele.

---

## Verificação (corpus real do hub)

Clone bare local do `hub-docs`, `cerne compile --force`:
- Páginas por módulo: finance 895 / identity 526 / marketplace 280 linhas — **úteis**.
- `ask "por que os módulos bills e investments foram absorvidos pelo finance" --history` → recuperou F-0021 (Finance Consolidation Bills+Investments), F-0025/F-0123/R-0003/D-0008 — **recall alto** (era ~zero).
- Página `project`: 7,5k linhas / **346 fontes** (44 decisões + 66 referências + 236 features **sem módulo**, 217 delas implemented). Bounded, mas grande.

## Descoberta → case 0006

O dogfood respondeu a pergunta em aberto "A agrava B?": **sim**, porque o hub não atribui módulo aos cases raiz (`docs/process/cases/`), então 236 features caem todas na página `project`. Isso é **granularidade de página / modelagem de módulo**, distinto de A/B/C — extraído para o [case 0006](../0006_wiki-page-granularity/spec.md): split de decisões/referências + tratamento de entidades sem módulo (split vs inferência de módulo vs não-sintetizar). Páginas por módulo e `ask` já são usáveis; o overview transversal espera o 0006.
