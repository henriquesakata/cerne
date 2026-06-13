# Case 0002 — Resultado

tags: #result #cerne #domain #lint #import

**Status da spec:** Implementado — suíte verde; **aceitação real no hub pendente** (1 passo manual, ver abaixo).
**Suíte:** 266 testes Go verdes em 16 pacotes (era 235; **+31**) · `go vet` limpo · `go build ./...` ok.
**Spec/plan:** [spec.md](spec.md) · [plan.md](plan.md). Brainstorming e plano em `277568b`.

---

## O que foi entregue

Os 6 milestones do [plan.md](plan.md), em TDD estrito (teste vermelho → verde → commit), um commit por preocupação:

| Milestone | Entrega | Commit |
|---|---|---|
| M1 | Domínio: `ValidateHistorical` + sentinels (`ErrNameRequired`/`ErrSupersededByRequired`/`ErrReasonRequired`/`ErrAliasTooShort`) + `MinAliasLen=5`; store: struct `Historical`, `Save/Load/LoadAllHistoricals`, prefixo `H-` no `idSubdirs` + `entities/historicals` no layout | `aa88f49`, `542d0bf` |
| M2 | App: `SaveHistoricalEntity` (núcleo sem commit, valida+NextID+persiste+evento `historical.recorded`) + `RecordHistorical` (núcleo + `CommitAndSnapshot`, source `cli`) | `879e896` |
| M3 | Lint: `buildNeedles` deriva needles de entidades históricas — ID/name (word-boundary) + um por alias (reusa o slot `imported`, guard `minTitleNeedleLen`); `Run` carrega `LoadAllHistoricals` | `93430f9` |
| M4 | CLI: `cerne retire <name> --by <id\|label> --reason <texto> [--alias a]...` (strings/erros pt-BR; `ErrAliasTooShort` traduzido) via `RecordHistorical` | `17cd528` |
| M5 | Import: parser `historical.toml` (`LoadHistoricalSeed`, BurntSushi/toml) + auto-detecção no `import` — dry-run lista, `--yes` pré-valida o seed inteiro (alias ruim aborta antes de qualquer escrita) e cria em lote sob o mesmo commit | `d031272`, `f5f51b8` |
| M6 | Index: históricas indexadas (`kind=historical`, body = aliases+reason) → `search` acha; `status --all` ganha seção **HISTÓRICAS** (+`historicals` no `--json`); regressão de materialização (históricas nunca em `.docs/`); **e2e** retire→lint | `46532ae`, `966cd48`, `6b8e49a`, `249496f` |

## Aceitação

- **Mecanismo provado por e2e automatizado** (`249496f`) ✅ — black-box por `cli.Root()`: `init → feature new → push de spec citando "boletos" → retire Bills (aliases bills/boletos, by Finance) → lint`. O `lint` sai com código ≠ 0 e nomeia `content/features/F-0001/spec.md`, o alias `boletos`, a entidade `H-0001` e o sucessor `Finance`. **É o gap Bills/Investments fechado de ponta a ponta.**
- **Históricas fora da materialização, dentro de search/status** ✅ — `test(materialize)` assere ausência em `.docs/`/`CONTEXT.md`; `search "boletos"` e `status --all` cobertos por teste.
- **Criação dupla idêntica** ✅ — `cerne retire` e seed `historical.toml` funilam pelo mesmo `SaveHistoricalEntity`.

**Pendente (1 passo manual, não rodável nesta sessão — sem acesso ao hub):** rodar contra o **Rockt Hub real** (`git@gitlab.com:rockt/hub-docs.git`): `cerne import` com um `historical.toml` semeando Bills/Investments → Finance, depois `cerne lint`, e confirmar os achados em `docs/planning/features.md` e em `R-0010`. Isso fecha **por completo** o critério nº 1 do case 0001. O e2e in-miniature já exercita exatamente esse caminho com um backend bare.

## Decisões tomadas durante a implementação

- **Sem novo tipo de estado**: `Historical.State` reusa `domain.StateSuperseded` (tipo `domain.FeatureState`, underlying string) — flui por index/search/status sem caso especial. Leve abuso de tipo, registrado como dívida.
- **Materialização não mudou**: ela itera só features/decisions/references; o `Kind` novo fica fora por construção. Travado por teste de regressão, não por código de exclusão.
- **Aliases reusam o slot `imported`** do needle do lint — encaixe natural: `refFindings` já produz o detalhe "menciona <alias> (<id> "<name>")".
- **Seed pré-validado em bloco** antes de qualquer escrita → falha de um alias aborta sem persistir nada (atomicidade prática, sem rollback de git).
- **`historical.toml` em TOML** (não YAML): zero dependência nova (BurntSushi/toml já no projeto).

## Desvios do plano

- Nenhum estrutural. Ajuste pontual: o resumo do `import` passou a incluir `, N históricas` (e o dry-run uma linha `Históricas: N`), o que exigiu atualizar a asserção do `TestImportYes` existente — mudança de formato consciente.

## Pendências e dívidas (para os próximos cases)

1. ✅ **Aceitação real no hub** — feita em 2026-06-13 (ver § Dogfood real abaixo); critério nº 1 do case 0001 fechado.
2. **Seed sem idempotência**: rodar `import` 2× com o mesmo `historical.toml` cria `H-` duplicados (o dry-run mostra o que será criado, mas não há dedup por `name`). Candidato a follow-up: dedup por nome/alias.
3. **`Historical.State` como `domain.FeatureState`**: abuso de tipo aceito por reuso; extrair um `domain.EntityState` se incomodar.
4. **Deferred da spec, intactos**: `cerne lint --history-gaps` (descoberta de candidatos); config de alias em `.cernerc` (min-length/denylist) — `MinAliasLen` segue hardcoded em 5; wiki-links `[[nota]]` no lint (pendência nº 2 do case 0001).

## Inventário (resumo)

- **Novos**: `internal/domain/historical.go`, `internal/app/historical.go`, `internal/cli/retirecmd.go`, `internal/importer/seed.go`, `e2e/historical_test.go` (+ testes irmãos em domain/store/app/lint/cli/index/materialize).
- **Tocados**: `internal/store/{entity,layout}.go` (struct + `H-` + subdir), `internal/lint/lint.go` (`buildNeedles`+`Run`), `internal/cli/{root,importcmd,statuscmd}.go`, `internal/index/index.go` (`collectRows`).
- Sem dependências novas.

## Dogfood real no hub (2026-06-13)

Rodado contra um **clone bare local** do backend `hub-docs` (308 features / 45 ADRs / 66 refs), sem tocar o GitLab.

- `cerne lint` baseline: **204** achados (18 ref-to-superseded, 25 broken-link, 161 missing-gate-doc) — idêntico ao case 0001.
- Após `cerne retire "Bills" --by Finance --alias bills --alias boletos` (e idem Investments): ref-to-superseded **18 → 89** (+71), total 275.
- **Alvos do critério atingidos:** `R-0010` (arquitetura — diagrama com as caixas `Bills`/`Investments`) e `R-0023` ("Features — Rockt Hub"). Amostras confirmam matches genuínos, ex.: ADR `D-0001` — "Independent feature apps (Finance, Bills, Investments)". **Critério nº 1 do case 0001 fechado.**
- Distribuição: 71 achados em 37 arquivos (26 decisões, 36 refs, 8 features). Aliases "bills"/"investments" são palavras comuns → parte pode ser falso-positivo (ex.: "boletos" como feature viva do Finance). Curadoria manual dos achados é o passo seguinte; reforça o risco de ruído de alias já previsto na spec.

## Próximos passos sugeridos

1. Curar os 71 achados no hub (separar referências reais a módulo morto de usos vivos do termo).
2. Avaliar dedup de seed (pendência 2) se o onboarding de legado for recorrente.
3. Seguir para o case 0003 (LLM: `compile`/`ask`).
