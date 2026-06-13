# Case 0006 — Granularidade da wiki para entidades sem módulo

tags: #spec #cerne #stub #llm #wiki #compile

**Status:** Draft (stub — precisa de rodada de brainstorming para fechar decisões)
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

## Direção a explorar (alternativas — decidir no brainstorming)

1. **Split das páginas transversais** (a opção B1 que ficou de fora do 0005): `content/wiki/decisions.md` (decisões ativas) + `content/wiki/references.md` (referências vigentes), tirando-as do `project`. Resolve 110 das 346 fontes. Toca `compile.Pages()`, o frontmatter (`cerne_wiki: decisions|references`), o `ask.Retrieve` (passar a ler essas páginas quando houver hit de decisão/referência) e o lint de wiki (genérico — deve validar sem mudança).
2. **Tratamento de features sem módulo** (o grosso — 236):
   - (a) **Inferência de módulo no import**: derivar módulo dos cases raiz (por tag, área, ou heurística) — empurra o problema para a modelagem; muitos cases raiz são genuinamente transversais.
   - (b) **Não sintetizar features sem módulo** num projeto grande: ficam no `CONTEXT.md` (índice determinístico) + `search`, mas sem página de wiki; fallback de "sintetizar todas no project" só quando são poucas (projeto pequeno/sem módulos).
   - (c) **Bucket "uncategorized"** paginado por faixa de ID.
3. **Map-reduce / pré-sumário por entidade** (a opção B2 do 0005): resumo curto por entidade antes da síntese da página — melhora qualquer página grande, ao custo de mais chamadas LLM.

---

## Questões em aberto

1. Split de decisões/referências: o `ask` deve sempre incluir essas páginas, ou só quando houver hit do tipo?
2. Features sem módulo: inferir módulo (onde?), não-sintetizar, ou bucket? Qual o comportamento para projeto **sem módulos declarados** (hoje: tudo no project)?
3. O page-set deixa de ser puramente derivado de `project.json.modules` — como manter determinismo do dirty-tracking?
4. Vale acoplar com a dívida do 0005 (caminho síncrono/BYO-key) para validar map-reduce de ponta a ponta?

---

## Próximos passos

1. **Brainstorming** para escolher split + política de features sem módulo.
2. `plan.md` (TDD): page-set (split) → política de module-less → `ask.Retrieve` → re-dogfood no hub (medir o tamanho das páginas transversais).
3. `result.md`.
