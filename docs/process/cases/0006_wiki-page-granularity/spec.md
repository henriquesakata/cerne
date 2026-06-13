# Case 0006 — Granularidade da wiki para entidades sem módulo

tags: #spec #cerne #llm #wiki #compile

**Status:** Done — implementado e verificado no corpus do hub (2026-06-13); ver [result.md](result.md)
**Tipo:** Granularidade de página da wiki (compile) + modelagem de módulo. Follow-up do case 0005.
**Origem:** Dogfood do case 0005 (2026-06-13). Com `implemented` incluído na wiki (Fix A do 0005) e o excerpt limitado (Fix B), a página transversal `project` do hub ainda ficou com **346 fontes** (7,5k linhas) — grande demais para uma síntese boa.

---

## Problema

A wiki do case 0003/0005 tem um page-set fixo: **uma página por módulo** (features daquele módulo) + **uma página `project`** transversal (decisões ativas + referências vigentes + features **sem módulo**). Isso assume que features têm módulo.

No hub isso quebra: o importador (case 0005, `import`) **não atribui módulo aos cases raiz** (`docs/process/cases/NNNN`), só aos de `apps/<app>/...`. Resultado medido no dogfood — a página `project` tem **346 fontes**:

| Tipo | Qtde |
|---|---|
| decisões ativas | 44 |
| referências vigentes | 66 |
| features **sem módulo** | 236 (217 implemented, 13 candidate, 6 in_development) |

O excerpt-bounding (0005-B) impede o estouro, mas 346 fontes numa página produzem uma síntese ruim. Decisões (44) e referências (66) também mereciam páginas próprias e focadas.

---

## Objetivo e critérios de sucesso (rascunho)

Wiki **usável** mesmo em projetos com muitas entidades sem módulo:
- A página transversal deixa de ser um despejo de centenas de fontes desconexas.
- Decisões e referências ganham síntese focada.
- Coberta a pergunta: o que fazer com features sem módulo num projeto grande.

---

## Decisões tomadas (2026-06-13)

Princípio: **a wiki sintetiza agrupamentos coerentes (módulos) + as camadas transversais (decisões, referências).** Um amontoado de cases raiz desconexos não é coerente — sintetizá-lo (mesmo paginado) produz ruído. A *cobertura completa* já é papel do `CONTEXT.md` (índice determinístico) + `search`; a wiki é a camada de *síntese*.

| Fork | Decisão | Consequência |
|---|---|---|
| **Páginas transversais** | **Split**: `content/wiki/decisions.md` (decisões ativas) + `content/wiki/references.md` (referências vigentes), fora do `project` | Cada uma focada e limitada (44 / 66 fontes no hub) |
| **Features sem módulo** | Projeto **com** módulos ⇒ **não sintetizadas** na wiki (cobertas por `CONTEXT.md`/`search`); `compile` imprime **nota com a contagem** (sem drop silencioso). Projeto **sem** módulos ⇒ fallback original: `project.md` com todas as features | Toda página gerada vira coerente; o hub para de despejar 236 fontes; transparência sobre o que não entrou |
| **`ask`** | Passa a ler `decisions.md`/`references.md` quando há hit do tipo, além de `project` + páginas de módulo | Recuperação cobre as novas páginas |
| **Inferência de módulo** | **Fora de escopo** → case 0007 | É modelagem/heurística (case-name→módulo), não granularidade; arriscado para cravar aqui |
| **Map-reduce** | Fora de escopo | O split já basta para tornar as transversais usáveis; map-reduce reabre a dívida do caminho síncrono |

Determinismo do dirty-tracking preservado: o page-set continua derivado de `project.json.modules` + as páginas fixas (`decisions`, `references`, e `project` só no modo sem-módulos); cada página com seu `input_hash`.

---

## Verificação (corpus real do hub)

Clone bare local do `hub-docs`, `cerne compile --force` (resultados no [result.md](result.md)): `decisions.md` e `references.md` passam a ser páginas próprias e limitadas; as 236 features sem módulo deixam de inflar o `project` (nota de contagem impressa); páginas por módulo inalteradas; `ask` lê as novas páginas.
