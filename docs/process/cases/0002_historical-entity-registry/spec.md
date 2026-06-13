# Case 0002 — Registro histórico de entidades

tags: #spec #cerne #stub #lint #import

**Status:** Draft (stub — precisa de rodada de brainstorming para fechar decisões)
**Tipo:** Extensão do modelo de domínio + lint + import.
**Origem:** Pendência nº 1 do [result.md do case 0001](../0001_founding-spec/result.md). Surgiu da rodada de aceitação real contra o Rockt Hub (2026-06-12): o lint flagrou 18 referências reais a entidades supersedidas, mas **não** flagrou as menções a "Bills"/"Investments" — porque a absorção desses módulos pelo Finance **precede** a convenção de cases do hub, então não existe entidade `cerne` para supersedir e, sem entidade, não há needle para o lint casar.

---

## Problema

O lint `ref-to-superseded` (regra determinística do case 0001) só consegue flagrar menções a uma entidade quando:
1. existe uma entidade `cerne` (Feature/Decision/Reference) com estado terminal (`superseded`/`rejected`/`obsolete`), e
2. essa entidade tem needles deriváveis (ID `cerne`, título ≥ 4 chars, ou identificadores originais via `imported_from`).

Quando algo foi **absorvido/descontinuado antes de existir documentação importável** (caso Bills → Finance, Investments → Finance), não há entidade para registrar a supersedência. As menções vivas (`docs/planning/features.md` ainda lista Bills/Investments como módulos; `R-0010` desenha `bills_db`/`investments_db` na arquitetura) passam despercebidas — exatamente o tipo de obsolescência que o produto promete pegar.

Generalizando: **falta um caminho para declarar "a entidade X existiu e foi supersedida/absorvida por Y", mesmo quando X nunca teve spec/case próprio.**

---

## Objetivo e critérios de sucesso (rascunho)

Permitir registrar entidades **históricas/supersedidas** com seus identificadores originais (aliases), de modo que o lint passe a flagrar referências vivas a elas.

Critério de aceite-alvo (fecha o critério nº 1 do case 0001 por completo):
- Após registrar Bills e Investments como entidades históricas supersedidas pelo Finance (com aliases `bills`, `boletos`?, `investments`, `investimentos`?), `cerne lint` no hub flagra as menções em `features.md` e `R-0010`.
- Entidades históricas **não** aparecem na materialização `.docs/` (são history), mas **aparecem** em `cerne search` e `cerne status --all`.

---

## Direção a explorar (alternativas — decidir no brainstorming)

1. **Flag no `feature new`**: `cerne feature new "Bills" --historical --superseded-by F-XXXX --reason "absorvido pelo Finance" --alias bills --alias boletos`. Cria a entidade já em estado terminal, sem exigir spec (gate relaxado para `--historical`).
2. **Comando dedicado** `cerne retire <título> --by <id> --alias ...` ou `cerne alias`. Separa a semântica "isto é registro histórico" do fluxo normal de features.
3. **Seed/import de aliases**: um arquivo declarativo (ex.: `historical.yaml`) lido pelo `import` que cria as entidades históricas em lote. Bom para onboarding de projetos legados.

Pontos transversais a decidir:
- **Aliases como dado de primeira classe**: estender as entidades com `aliases []string`? O lint passaria a derivar needles também dos aliases (com o guard de ≥ 4 chars / word-boundary já existente).
- Precisa de um novo `Kind`/estado, ou basta uma Feature/Decision em estado terminal + `aliases`?
- Como o `superseded_by` aponta para algo que pode não ter ID `cerne` ainda (ex.: "Finance" é um módulo, não uma feature)?

---

## Questões em aberto

1. Alias é atributo da entidade ou tabela separada? (impacta store + index + lint)
2. `--historical` relaxa quais gates exatamente, e como o evento (`feature.imported`/`feature.retired`?) registra isso?
3. Risco de ruído: aliases curtos/genéricos ("one", "core") gerando falsos positivos no lint — reusar o guard de comprimento e word-boundary do case 0001; talvez exigir alias ≥ 5 chars.
4. Vale um relatório `cerne lint --history-gaps` que sugira candidatos a entidade histórica (termos frequentes em docs vivas que não casam com nenhuma entidade)?

---

## Próximos passos

1. Rodada de **brainstorming** para escolher entre as alternativas e fechar o modelo de aliases.
2. `plan.md` (TDD): provavelmente domínio (aliases) → store → lint (needles de alias) → CLI (`--historical`/comando) → re-rodar aceitação no hub.
3. `result.md`.
