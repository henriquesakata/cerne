# Case 0027 — Épicos: relação pai/filho nativa entre features

tags: #spec #cerne #epics #hierarchy #features #graph #ui

**Status:** Rascunho (2026-06-16) — spec para revisão; `plan.md` e `result.md` a seguir.
**Tipo:** Relação **pai/filho** nativa entre features. Um *épico* não é entidade nova: é uma **feature que tem filhos** (papel **inferido**, sem flag booleana). Modelado como um campo `Parent` na feature — espelhando exatamente `SupersededBy` — e projetado como aresta `part_of` no grafo existente. Núcleo **determinístico**, sem LLM. Cobertura completa: dado + validação + CLI + rollup em `status`/`CONTEXT.md` + `lint` + API + UI.
**Origem:** Constatação de um agente durante trabalho no backend: *"Não há flag de épico/parent nativa"*. Decisão do usuário (2026-06-16): implementar de forma **completa para as demandas de documentação** (não superfície mínima), com épico **inferido** (feature com filhos) e **núcleo + UI no mesmo case**.

---

## Problema

O cerne rastreia trabalho na granularidade de **feature** (a unidade com `spec`→`plan`→`result` e ciclo de vida próprio), mas **não há como expressar que uma feature grande se decompõe em features menores**. Hoje `store.Feature` tem `ID, Title, Module, State, SupersededBy, Reason, …` ([entity.go:22](../../../internal/store/entity.go)) — nenhum campo de hierarquia. Buscas por `parent`/`epic`/`children` na base só acham "diretório pai" e a prop `children` do React; o conceito de domínio não existe.

`Module` **não** resolve isso: módulo é o **subsistema** (agrupamento plano, inferido por LLM — cases 0007/0018). Um épico é uma **unidade de trabalho** decomposta em filhas, que pode caber num módulo só **ou cruzar módulos**, e que tem ciclo de vida próprio. São ortogonais: uma feature pode estar no módulo `billing` **e** ser filha do épico `F-0042 "Revamp de assinaturas"`.

Consequências da ausência:
- O roadmap de um trabalho grande vira **prosa** (uma lista de features soltas num doc), exatamente a staleness que o cerne existe para matar (problema #3 do spec fundador). Não há um "estado do épico" derivado dos dados.
- O grafo explicativo (case 0021) desenha `superseded_by`/`informs`/`links`, mas **não a decomposição** — a relação mais natural de "esta feature faz parte daquela maior" fica invisível.
- `cerne status` e o `CONTEXT.md` (o roster que os agentes leem) listam features achatadas por estado, sem mostrar **a quebra de trabalho** nem o progresso de um épico.

## Objetivo e critérios de sucesso

1. **Dado (store):** `store.Feature` ganha `Parent string` (`json:"parent,omitempty"`) — o ID da feature-épico. Opcional; vazio = feature de topo. Idêntico em forma a `SupersededBy`.
2. **Épico é inferido:** uma feature é épico **sse e somente se** alguma outra feature a referencia em `Parent`. Sem campo booleano, sem novo estado — igual a "superseded", que é derivado do campo, não declarado.
3. **Invariantes (domain, puro):** ao definir/alterar o pai, o cerne **garante**: o pai existe e é uma feature; pai ≠ a própria feature; **sem ciclo** (subir a cadeia de pais a partir do pai proposto nunca pode reencontrar o filho); o pai **não** está terminal (`superseded`/`rejected`) no momento da atribuição. Profundidade arbitrária é permitida (floresta), o que cobre "iniciativa → épico → feature".
4. **CLI:**
   - `cerne feature new <title> --parent F-xxxx [--module …]` cria já vinculada.
   - `cerne feature reparent <id> <parent-id>` move; `cerne feature reparent <id> --clear` desvincula.
   - `cerne status` ganha uma seção **ÉPICOS** (quando houver algum): cada épico com seus filhos vivos e um **progresso** `n/m implementados`. O roster passa a mostrar a quebra de trabalho.
5. **Materialização (`CONTEXT.md`):** o roster que os agentes leem ([materialize.go](../../../internal/materialize/materialize.go)) ganha (a) uma coluna **Épico** na tabela de Features (o ID do pai, vazio no topo) e (b) uma seção **`## Épicos`** com a árvore de decomposição vigente + progresso. Determinístico; entra no golden de `CONTEXT.md`.
6. **Grafo:** nova aresta `part_of` (filho → pai) em `app.BuildGraph` ([graph.go](../../../internal/app/graph.go)); participa do layout em camadas (a hierarquia *é* a explicação, como `superseded_by`). Render na UI com estilo/rótulo próprios ("parte de").
7. **Lint (determinístico):** novas regras de staleness — `parent-not-found` (pai inexistente), `parent-terminal` (feature viva sob épico terminal), `epic-open-children` (épico `implemented` com filhos ainda abertos). Sem LLM; o cerne **sinaliza** inconsistência em vez de **travar** transições.
8. **API + UI:**
   - `POST /api/features` aceita `parent`; novo `PUT /api/features/{id}/parent`.
   - `GET /api/features/{id}` (FeatureDetail) passa a trazer os **filhos diretos**.
   - **Página da feature:** breadcrumb "Épico: F-xxxx" quando há pai; seção **Filhos** com chips de estado + barra de progresso; controle para definir/trocar/remover o épico.
   - **Kanban:** badge "épico" nos cards que têm filhos, indicação do pai nos filhos, e **agrupamento por épico** (toggle de swimlanes) além do agrupamento por estado.
   - **Nova feature:** seletor de épico (features não-terminais).
9. **Eventos:** `feature.created` registra `parent` quando definido; nova entrada `feature.reparented` (payload `from`/`to`).
10. `go vet` + suíte verdes; build da UI ok.

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Entidade nova `Epic` vs. reutilizar Feature** | **Reutilizar Feature** + campo `Parent` | É a opção *mais completa*, não a mínima: o épico herda de graça todo o aparato documental (ciclo de vida, `spec`/`plan`/`result`, conteúdo indexado por `ask`/`search`, supersessão com motivo, nó no grafo, backlinks, página na UI). Uma entidade nova começaria **incompleta** e duplicaria FSM/materialização/lint/índice/UI. Conceitualmente um épico *é* uma feature maior, decomposta. |
| **Épico explícito (flag) vs. inferido** | **Inferido** (épico = feature com ≥1 filho) | Zero estado novo; coerente com "papéis emergem das arestas" (não há booleano `is_superseded`). Escolha do usuário. |
| **Nome do campo/aresta** | Campo `Parent`; aresta `part_of` (filho → pai) | `Parent` espelha o modelo mental e `SupersededBy`. `part_of` lê natural na convenção das arestas ("F-007 *part_of* F-001"); rótulo pt-BR "parte de". |
| **Profundidade** | Floresta de profundidade arbitrária; ciclo proibido | Cobre iniciativa→épico→feature sem custo extra; o guard de ciclo dá a segurança. "Épico" é definido frouxamente como "qualquer feature com filhos". |
| **Ciclo/integridade** | Validar **na escrita** (create/reparent) + **lint** como rede de segurança | Comandos gated não conseguem persistir ciclo/pai-inexistente; o `lint` cobre o que entra por `import`/edição manual. |
| **Ciclo de vida do épico** | **Independente**; sem gate de "concluir épico exige filhos prontos" | O usuário quer completude **documental**, não imposição de processo. O cerne **sinaliza** (lint `epic-open-children`) em vez de **travar** — fiel ao "lifecycle é dado, lint flagra staleness". |
| **Pai terminal** | **Rejeitar** atribuir pai terminal; `lint` flagra pai que *fica* terminal depois | Não se arquiva trabalho novo sob épico morto; mas um épico substituído **depois** deixa os filhos órfãos → staleness navegável (regra `parent-terminal`). |
| **Cross-entity** | Pai é sempre Feature→Feature | Decisão/referência não são unidades de decomposição de trabalho. |
| **Persistência do rollup** | Calcular **on-the-fly** sobre `LoadAllFeatures`; sem tabela no índice | Mesma decisão do grafo (0021): dezenas–centenas de entidades; montar é barato. Adjacência materializada = outro case. |
| **Auto-reparent ao substituir épico** | **Fora do v1** | Útil (oferecer no fluxo `substituir` mover os filhos para o sucessor), mas aditivo; o `lint` já torna o caso visível. |
| **`import` detectar épicos** | **Fora do v1** | Precisa de uma convenção no hub que ainda não existe; quando existir, vira backfill próprio. |

---

## Desenho

### Dados — `internal/store`, `internal/domain`
- `store.Feature`: novo `Parent string \`json:"parent,omitempty"\``. Sem mudança em `Save/Load` (genéricos).
- `internal/domain` (puro; **não** importa `store`): validador sobre um mapa `parentOf map[string]string` (id→pai):
  - `var ErrParentSelf`, `var ErrParentCycle` (sentinelas wrap-áveis).
  - `func ValidateParentLink(childID, newParent string, parentOf map[string]string) error` — auto-referência e ciclo (sobe a cadeia a partir de `newParent`; se reencontrar `childID`, ciclo).

### App — `internal/app`
- `CreateFeature(repoDir, projDir, title, module, parent string)` (novo parâmetro): se `parent != ""`, valida existência + não-terminal (carrega via `store.LoadFeature`) e `domain.ValidateParentLink`; grava `f.Parent`; o evento `feature.created` inclui `parent`.
- `SetFeatureParent(repoDir, projDir, id, parent string) error` (novo): valida igual ao create (com `parent == ""` = desvincular, sempre válido), atualiza `f.Parent` + `UpdatedAt`, anexa `feature.reparented` (`from`/`to`), `CommitAndSnapshot`. Sentinelas de app: `ErrParentNotFound`, `ErrParentTerminal`.
- `internal/app/epic.go` (novo, puro sobre `[]store.Feature`):
  - `func ChildrenByParent(features []store.Feature) map[string][]store.Feature` (filhos por ID de pai, já ordenados por ID).
  - `func EpicProgress(children []store.Feature) (done, total int)` — `done` = filhos `implemented`. Reusado por `status`, `materialize` e API.

### Grafo — `internal/app/graph.go`
- `const EdgePartOf = "part_of"`. Para cada feature com `Parent != ""`: `addEdge(f.ID, f.Parent, EdgePartOf, "")` (alvo inexistente é descartado pela checagem existente; sem dangling).
- Ordenação determinística inalterada.

### CLI — `internal/cli`
- `feature new`: flag `--parent`; passa a `CreateFeature`.
- `feature reparent <id> <parent-id>` + `--clear` ([reparentcmd.go] novo, registrado em `newFeatureCmd`); chama `SetFeatureParent`. Saídas pt-BR (ex.: `"%s agora é filha de %s."` / `"%s desvinculada do épico."`).
- `cerne status` ([statuscmd.go](../../../internal/cli/statuscmd.go)): após o total, seção `ÉPICOS (n)` quando houver épicos com filhos vivos — por épico, a linha `F-xxxx Título — n/m implementados` e os filhos indentados com estado. `--json` ganha o campo `parent` por feature (já vem de `store.Feature`).

### Materialização — `internal/materialize/materialize.go`
- `contextTemplate`/`featureRow`: coluna **Épico** (ID do pai, vazio no topo).
- Nova seção `## Épicos` após `## Features`: para cada épico com ≥1 filho **vigente** (respeitando o filtro `scope`), o título + progresso e a lista de filhos (ID, estado, título). `_Nenhum._` quando vazio.
- Atualizar o golden `TestMaterializeContextGolden` e os testes de seção.

### Lint — `internal/lint/lint.go`
- Novas constantes em `RuleOrder`/`ruleRank`: `RuleParentNotFound = "parent-not-found"`, `RuleParentTerminal = "parent-terminal"`, `RuleEpicOpenChildren = "epic-open-children"`.
  - `parent-not-found`: `Parent` aponta para ID inexistente.
  - `parent-terminal`: feature **viva** (candidate/approved/in_development) cujo pai está `superseded`/`rejected`.
  - `epic-open-children`: feature `implemented` com ≥1 filho não-terminal (épico marcado pronto, decomposição ainda aberta).
- Determinísticas; detalhe em pt-BR; entram na ordem do relatório.

### Server — `internal/server`
- `POST /api/features`: corpo aceita `parent` → `CreateFeature(..., parent)`.
- `PUT /api/features/{id}/parent`: corpo `{ "parent": "F-xxxx" }` (vazio desvincula) → `SetFeatureParent`; devolve a feature atualizada. Erros mapeiam os sentinelas (`ErrParentNotFound`/`ErrParentTerminal`/`ErrParentSelf`/`ErrParentCycle`) para HTTP 4xx com mensagem pt-BR.
- `GET /api/features/{id}` (FeatureDetail DTO): novo `children []Feature` (filhos diretos, ordenados por ID). `parent` já viaja embutido em `feature`.

### UI — `ui/src`
- `api.ts`: `Feature.parent?: string`; `FeatureDetail` ganha `children?: Feature[]`; `createFeature(title, module, parent?)`; novo `setFeatureParent(id, parent)`; `GraphEdgeType` ganha `'part_of'`.
- `FeaturePage.tsx`: no header, "Épico: \<link F-xxxx\>" quando `f.parent`; seção **Filhos** (chips de estado + barra de progresso `n/m`, cada filho linkando à sua página); controle para definir/trocar/remover o épico (`setFeatureParent`).
- `Kanban.tsx`: badge "épico" no card que tem filhos; `↳ F-xxxx` no card-filho; toggle **"Agrupar por épico"** (swimlanes por épico + lane "Sem épico", cards agrupados por estado dentro de cada lane), preservando o toggle "Mostrar encerradas".
- `NewFeature.tsx`: seletor de épico (features não-terminais) opcional.
- `Graph.tsx`: estilo/rótulo da aresta `part_of` ("parte de"); entra no layout em camadas junto de `superseded_by`.

---

## Verificação
- **store/domain (TDD):** round-trip de `Parent`; `ValidateParentLink` cobre auto-referência, ciclo de 2 e 3 nós, e cadeia válida profunda (sem falso positivo).
- **app:** `CreateFeature`/`SetFeatureParent` — pai inexistente → `ErrParentNotFound`; pai terminal → `ErrParentTerminal`; auto/ciclo → sentinelas de domínio; reparent feliz emite `feature.reparented`; `--clear` sempre vale. `ChildrenByParent`/`EpicProgress` em tabela.
- **graph:** fixture com `F-0002.Parent=F-0001` → aresta `part_of` F-0002→F-0001; pai inexistente não vira aresta; dedup/ordenação.
- **cli:** `feature new --parent`; `feature reparent`/`--clear`; `status` renderiza a seção ÉPICOS com progresso correto.
- **materialize:** golden de `CONTEXT.md` com coluna Épico + seção `## Épicos` (com e sem épicos; respeitando `scope`).
- **lint:** fixtures disparam `parent-not-found`, `parent-terminal`, `epic-open-children`; ordem do relatório.
- **server (httptest):** `POST /api/features` com `parent`; `PUT …/parent` feliz e erros; `GET …/{id}` traz `children`.
- **ui:** build ok com o tipo novo; página mostra pai/filhos/progresso; kanban agrupa por épico; grafo desenha `part_of` (revisão + build).
- `go vet` + suíte verdes.

## Fora de escopo
- **Gate** de "concluir épico exige filhos prontos": ciclo de vida independente; `lint` sinaliza (`epic-open-children`).
- **Auto-reparent dos filhos** quando o épico é substituído (oferecer no fluxo `substituir`): aditivo, follow-up.
- **`import` detectar épicos** de convenções do hub: precisa da convenção existir primeiro.
- **Tabela de adjacência materializada** no índice SQLite: otimização de escala; v1 monta on-the-fly (como o grafo).
- **Pais cross-entity** (decisão/referência como pai): épicos são Feature→Feature.
