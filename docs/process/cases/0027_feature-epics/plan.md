# Case 0027 — Plano de implementação

Decomposto em commits TDD (teste vermelho → implementação mínima → verde → commit), **uma preocupação por commit**, na ordem de dependência: dado → domínio → app → grafo → CLI → materialização → lint → server → UI. A suíte deve ficar verde a cada commit.

Convenções: Conventional Commits; corpo termina com `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`. Strings de usuário em pt-BR; identificadores/código em inglês.

---

## 1. store — campo `Parent`
- **Teste:** round-trip de `store.Feature{Parent: "F-0001"}` (save→load) preserva o campo; feature sem pai serializa **sem** a chave (`omitempty`).
- **Impl:** `Parent string \`json:"parent,omitempty"\`` em `store.Feature` ([entity.go](../../../internal/store/entity.go)).
- `feat(store): add Parent field to Feature`

## 2. domain — validação pura de vínculo
- **Teste:** `ValidateParentLink(child, newParent, parentOf)` — auto-referência → `ErrParentSelf`; ciclo direto (A→B, definir B.parent=A) e indireto (A→B→C, definir C.parent=A... i.e. A já é descendente) → `ErrParentCycle`; cadeia válida profunda → nil; `newParent` ausente do mapa → nil (existência é do app).
- **Impl:** `internal/domain/epic.go` (novo): `ErrParentSelf`, `ErrParentCycle`; `ValidateParentLink(childID, newParent string, parentOf map[string]string) error` (sobe a cadeia a partir de `newParent`; reencontrar `childID` = ciclo). Puro, não importa `store`.
- `feat(domain): add ValidateParentLink for parent/child invariants`

## 3. app — criar/reparentar + rollup
- **3a. epic.go (puro):** `ChildrenByParent([]store.Feature) map[string][]store.Feature` (ordenado por ID) e `EpicProgress([]store.Feature) (done, total int)` (done = `implemented`). Tabela.
  - `feat(app): add epic rollup helpers (ChildrenByParent, EpicProgress)`
- **3b. CreateFeature com parent:** novo parâmetro `parent`; quando não-vazio: existência (`store.LoadFeature` → `ErrParentNotFound`), não-terminal (`ErrParentTerminal`), `domain.ValidateParentLink` sobre o mapa carregado; grava `f.Parent`; evento `feature.created` inclui `parent`. Atualizar chamadores (CLI/server) para passar `""`.
  - **Teste:** feliz; pai inexistente; pai terminal; auto/ciclo.
  - `feat(app): accept parent on CreateFeature with validation`
- **3c. SetFeatureParent:** valida igual (com `parent==""` = desvincular, sempre válido); atualiza `f.Parent`+`UpdatedAt`; anexa `feature.reparented` (`from`/`to`); `CommitAndSnapshot`. Sentinelas `ErrParentNotFound`/`ErrParentTerminal` (app).
  - **Teste:** reparent feliz emite evento; clear; erros.
  - `feat(app): add SetFeatureParent (reparent/clear)`

## 4. graph — aresta `part_of`
- **Teste:** fixture `F-0002.Parent=F-0001` → aresta `{F-0002,F-0001,"part_of",""}`; pai inexistente não vira aresta; dedup/ordenação.
- **Impl:** `EdgePartOf = "part_of"` + loop em `BuildGraph` ([graph.go](../../../internal/app/graph.go)).
- `feat(graph): project part_of edges from feature parents`

## 5. CLI
- **5a. `feature new --parent`** ([featurecmd.go](../../../internal/cli/featurecmd.go)): flag → `CreateFeature(..., parent)`.
  - `feat(cli): add --parent to feature new`
- **5b. `feature reparent <id> <parent>` + `--clear`** (`reparentcmd.go` novo, em `newFeatureCmd`): chama `SetFeatureParent`; saídas pt-BR.
  - `feat(cli): add feature reparent command`
- **5c. `status` seção ÉPICOS** ([statuscmd.go](../../../internal/cli/statuscmd.go)): após o total, `ÉPICOS (n)` com `F-xxxx Título — n/m implementados` + filhos indentados (estado), via helpers de `app`. Só épicos com filhos vivos.
  - `feat(cli): show epic rollup in status`

## 6. materialize — CONTEXT.md
- **Teste:** golden `TestMaterializeContextGolden` atualizado: coluna **Épico** na tabela + seção `## Épicos` (com/sem épicos; respeitando `scope`).
- **Impl:** ajustar `contextTemplate`/`featureRow`/`contextData` + montar a seção em `Materialize` ([materialize.go](../../../internal/materialize/materialize.go)) com `app.ChildrenByParent`/`EpicProgress`. (Cuidado com ciclo de import app↔materialize — se houver, replicar os helpers puros ou movê-los para um pacote neutro; decidir no commit.)
- `feat(materialize): surface epics in CONTEXT.md`

## 7. lint — 3 regras
- **Teste:** fixtures disparam `parent-not-found`, `parent-terminal`, `epic-open-children`; ordem do relatório.
- **Impl:** constantes em `RuleOrder`/`ruleRank` + checagens determinísticas ([lint.go](../../../internal/lint/lint.go)).
- `feat(lint): add parent/epic staleness rules`

## 8. server — API
- **8a.** `POST /api/features` aceita `parent`; `GET /api/features/{id}` traz `children []Feature`. Testes httptest.
  - `feat(server): accept parent on create and expose children`
- **8b.** `PUT /api/features/{id}/parent` → `SetFeatureParent`; mapeia sentinelas para 4xx pt-BR. Teste feliz + erros.
  - `feat(server): add reparent endpoint`

## 9. UI
- **9a. api.ts:** `Feature.parent?`, `FeatureDetail.children?`, `createFeature(...parent?)`, `setFeatureParent`, `GraphEdgeType += 'part_of'`.
- **9b. FeaturePage.tsx:** breadcrumb do épico; seção **Filhos** (chips + progresso); controle definir/trocar/remover épico.
- **9c. Kanban.tsx:** badge "épico", `↳ pai` nos filhos, toggle "Agrupar por épico" (swimlanes).
- **9d. NewFeature.tsx:** seletor de épico.
- **9e. Graph.tsx:** estilo/rótulo `part_of` no layout em camadas.
- **9f.** `npm ci && npm run build`; commitar `ui/dist`.
- `feat(ui): epics — parent/children, kanban grouping, graph edge`

## 10. Fechamento
- `go vet ./... && go test ./... -count=1` verdes; build da UI ok.
- Escrever `result.md` (números de aceite, débitos, próximos passos).
- Registrar no guardião: `cerne import` + `cerne pull`; commitar `docs.lock`.
- **Entrega:** fetch+rebase na `main`, renumerar o case se 0027 colidiu (worktrees paralelas).
