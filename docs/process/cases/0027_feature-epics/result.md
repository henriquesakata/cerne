# Resultado — Case 0027 (épicos: relação pai/filho entre features)

**Status:** Implemented (2026-06-16) — suíte verde, `go vet` limpo, UI compila; aguardando revisão e registro no guardião (`cerne import` + `pull`).

---

## O que foi entregue

Um épico **não é entidade nova**: é uma feature com filhos. Toda a relação se apoia num único campo `Parent` na feature (espelhando `SupersededBy`), com o papel "épico" **inferido** do link — sem flag booleana.

### Dado e domínio (puro, sem I/O)
- `store.Feature.Parent` (`json:"parent,omitempty"`) — ID do épico que a feature decompõe.
- `domain.ValidateParentLink(child, newParent, parentOf)` — invariantes da floresta: auto-referência (`ErrParentSelf`) e ciclo (`ErrParentCycle`), com guarda contra ciclo pré-existente no mapa. Existência é responsabilidade do app.
- `store.ChildrenByParent` / `store.EpicProgress` — projeções puras sobre `[]Feature` (filhos por épico, ordenados; progresso `implementados/total`). Ficam no `store` (não no `app`) para que `materialize` — projeção irmã — não precise importar `app`.

### App
- `CreateFeature(... parent)` valida pai: existência (`ErrParentNotFound`), não-terminal (`ErrParentTerminal`) e aciclicidade (helper `validateParent`); o evento `feature.created` carrega `parent`.
- `SetFeatureParent` reparenta/desvincula, emite `feature.reparented` (`from`/`to`), commita e atualiza o lock; no-op quando o pai não muda.

### Grafo
- Aresta `part_of` (filho → épico) em `BuildGraph`; alvo inexistente é descartado como as demais. Participa do layout em camadas (a hierarquia *é* a explicação).

### CLI
- `feature new --parent F-xxxx`.
- `feature reparent <id> <parent>` + `--clear`.
- `status` ganha a seção **ÉPICOS**: cada feature com filhos vivos, progresso `n/m implementadas` e os filhos indentados com estado.

### Materialização (CONTEXT.md — o roster que os agentes leem)
- Coluna **Épico** na tabela de Features + seção **`## Épicos`** com a quebra de trabalho vigente e progresso (filhos terminais excluídos). Golden e testes de seção atualizados.

### Lint (determinístico — sinaliza, não trava)
- `parent-not-found` (pai inexistente), `parent-terminal` (feature viva sob épico terminal), `epic-open-children` (épico `implemented` com filhos ainda abertos).

### API + UI
- `POST /api/features` aceita `parent`; `GET /api/features/{id}` devolve `children`; `PUT /api/features/{id}/parent` reparenta (404 p/ feature inexistente, 422 p/ vínculo inválido).
- **Página da feature:** breadcrumb "Épico: F-xxxx", painel **Filhos** (chips + progresso) e controle para definir/trocar/remover o épico (`<select>` de features elegíveis).
- **Kanban:** badge "épico" nos cards com filhos, marcador `↳ pai` nos filhos, toggle **"Agrupar por épico"** (swimlanes por épico + lane "Sem épico").
- **Nova feature:** seletor de épico opcional.
- **Grafo:** aresta `part_of` (azul sólida, "parte de") + cor no filtro/legenda; `ui/dist` reconstruído com `npm ci`.

**17 commits:** docs (spec+plan) → store(Parent) → domain(ValidateParentLink) → app(rollup) → app(create parent) → app(SetFeatureParent) → graph(part_of) → cli(--parent) → cli(reparent) → cli(status) → refactor(store: mover rollup) → materialize(CONTEXT) → lint(3 regras) → server(create+children) → server(reparent) → test(e2e CONTEXT) → ui.

## Aceitação
- `go vet ./...` limpo; suíte **491 passando** em 22 pacotes (testes focados em cada camada: round-trip do campo + omitempty; `ValidateParentLink` self/ciclo/cadeia válida; `ChildrenByParent`/`EpicProgress`; create e reparent felizes e em erro; aresta `part_of`; `--parent`/`reparent`/`status`; golden + seção de épicos do CONTEXT; 3 regras de lint; 3 endpoints).
- e2e golden-path atualizado para a nova coluna Épico; verde.
- Build da UI ok (tsc verde); `ui/dist` embutido via `go:embed` (go build verde).

## Débitos e próximos passos
1. **Registro no guardião:** `cerne import` + `cerne pull` (empurra todos os cases não importados — backend sincronizado até 0025; 0026 e 0027 entram juntos) e commitar o `docs.lock`. Ação outward-facing — fazer na entrega.
2. **Gate de conclusão do épico:** decisão de design foi **não travar**; `epic-open-children` sinaliza. Reavaliar se o uso pedir um gate opcional.
3. **Auto-reparent ao substituir um épico:** oferecer no fluxo `substituir` mover os filhos para o sucessor — hoje o `lint` (`parent-terminal`) só sinaliza. Aditivo.
4. **`import` detectar épicos** de convenções do hub: precisa da convenção existir primeiro.
5. **Adjacência materializada** no índice: v1 monta on-the-fly (como o grafo); otimização de escala é outro case.
6. **Smoke real do `cerne ui`/CLI** pendente (exige projeto+backend); cobertura automática vem do e2e in-process + httptest + testes de CLI.
7. **Entrega:** fetch+rebase na `main` e **renumerar** o case se `0027` colidiu com worktrees paralelas.
