# Case 0032 — `cerne ui` multi-projeto: um painel, N projetos

tags: #spec #cerne #ui #multiprojeto #sse #dx

**Status:** Rascunho (2026-06-19) — spec para revisão; `plan.md`/`result.md` a seguir.
**Tipo:** Evolutivo (DX). Estende o `cerne ui` (hoje single-project) para servir vários projetos a partir de uma única instância, sem mudar o modelo de fonte-da-verdade nem os handlers de leitura (que já são parametrizados por `repoDir`/`projDir`).
**Origem:** Quem desenvolve **vários projetos cerne em paralelo** quer um painel só para todos, em vez de uma porta/aba por projeto. Decisões de produto registradas com o dono: (a) **seleção global** de projeto (sem `pid` na URL); (b) o 2º `cerne ui` **fica atado** e se remove no Ctrl+C; (c) **badge** sinaliza mudança em projetos não-ativos.

---

## Problema

O `cerne ui` ([uicmd.go](../../../../internal/cli/uicmd.go)) resolve **um** projeto (do cwd) e serve via [`server.NewLive`](../../../../internal/server/server.go) em `localhost:7717`. Abrir um segundo projeto hoje exige outra porta e outra aba; não há visão unificada. Subir um 2º `cerne ui` na mesma porta simplesmente falha no bind.

Ao mesmo tempo, **a base multi-projeto já existe** no código:
- O `cerne serve` ([multiproject.go](../../../../internal/server/multiproject.go)) já serve rotas `/api/projects/{pid}/*`, resolvendo `(repoDir, projDir)` por `pid` a cada request e reusando os mesmos handlers.
- Os handlers de leitura (`handleState`, `handleFeatureDetail`, `handleLint`, …) **já recebem `repoDir`/`projDir` por parâmetro** — não há estado global de projeto no servidor.

A diferença: o `cerne serve` é para um **host remoto** (segredo S2S, projetos provisionados por **URL de backend** clonada em `CacheDir(pid)`), sem UI e sem live-refresh. O `cerne ui` multi-projeto é **local**: projetos resolvidos de **diretórios** que o dev já tem, sem segredo, com a UI embarcada e o live-refresh do caso 0031.

## Objetivo e critérios de sucesso

1. **Host + join por detecção de porta.** `cerne ui` no projeto A bina `:7717` e vira **host** (registra A). `cerne ui` no projeto B, com A no ar, detecta a porta em uso, **confirma que é um cerne ui**, registra B no host e **fica atado** (segura o terminal). A página de A passa a listar A **e** B **ao vivo, sem F5**.
2. **Seleção global na página.** Um seletor no **canto direito** do header: com 1 projeto, mostra só o nome; com N, um `select`. A escolha é **global** (persistida em `localStorage`, sincronizada entre abas via evento `storage`) — sem `pid` na URL. Trocar o projeto re-renderiza todas as seções (kanban, feature, decisões, …) com a doc do projeto escolhido.
3. **Badge de mudança.** Quando um projeto **não-ativo** avança, um marcador "atualizado" aparece ao lado dele no seletor; selecioná-lo limpa o badge e re-busca. O projeto **ativo** re-busca direto (comportamento do 0031).
4. **Ciclo de vida atado.** Ctrl+C no `cerne ui` de B faz **deregister**: B sai do seletor (ao vivo) e seu watcher para; A segue. Fechar o host (A) derruba o painel.
5. **Sem regressão.** `server.New`/`NewLive` single-project preservados (testes intactos). Live-refresh por projeto. `go vet` + suíte + `go build` verdes; UI rebuildada (`ui/dist` commitado).
6. **Local e seguro.** Bind em `localhost`; o modo cliente faz **probe** do `:7717` antes de registrar (não fala com outro app que por acaso ocupe a porta). Sem segredo S2S (é dev local).

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Gatilho de multi-projeto** | Detecção de porta: bind OK → **host**; `EADDRINUSE` → **cliente** que registra no host | Mesmo comando (`cerne ui`) em qualquer projeto; um único dono da porta/instância. |
| **Como o 2º registra** | `POST /api/projects {dir}`; o **host** resolve o `.cernerc` do dir (reusa `resolveProject`) | Projetos são locais; o host já roda na máquina e enxerga o dir. Evita o modelo de clone-por-URL do `cerne serve`. |
| **Quem avisa a página** | O **host** (dono do SSE) emite o evento; o 2º processo só faz o `POST`/`DELETE` | O 2º processo não tem conexão com a página; corrige o "envia evento pra página". |
| **Rotas** | Escopadas: `/api/projects/{pid}/*` (reusa os handlers atuais) | Handlers já parametrizados; seguro para múltiplas abas; espelha o `cerne serve`. |
| **Stream** | Um SSE multiplexado: `change` com `data: <pid> <sha>` + evento `projects` quando o registro muda | Uma conexão por aba; a página filtra por ativo (re-busca) ou marca badge; lista de projetos atualiza ao vivo. |
| **Seleção de projeto** | **Global** via `localStorage` + evento `storage`; **sem `pid` no hash** | Escolha do dono; mantém o roteamento de seção (`#/f/…`) intacto; todas as abas no mesmo projeto. |
| **Ciclo de vida do cliente** | Fica atado; **deregister no Ctrl+C** (`DELETE /api/projects/{pid}`) | Fechar o terminal do projeto o remove do seletor — lifecycle natural. |
| **Watcher/pull** | Por projeto no host, amarrados ao registro (cancelados no deregister) | Cada projeto tem seu HEAD; o caso 0031 roda por projeto. |
| **Segurança** | `localhost` + probe de identidade; **sem** segredo S2S | Dev local; o segredo do `cerne serve` existe para host remoto/multiusuário. |

---

## Desenho

### `internal/server` — servidor multi-projeto local com live-refresh
- **Registro** (`registry`): `map[pid]*projectEntry{ name, repoDir, projDir, cancel }` protegido por mutex. `add(dir)` resolve o projeto (via um resolvedor injetado — ver CLI), sobe `watchHead` (+ `pullLoop` se intervalo > 0) num contexto próprio e devolve `{pid, name}`. `remove(pid)` cancela o contexto e remove.
- **Hub multiplexado** (estende o `liveHub` do 0031): broadcast carrega `(pid, sha)`; novo broadcast `projectsChanged()`. `handleStream` emite `event: change\ndata: <pid> <sha>` e `event: projects\ndata: \n` (gatilho de re-fetch da lista). Mantém connect-resync e keep-alive.
- **Endpoints novos**:
  - `GET /api/projects` → `{ projects: [{pid, name}], default: <pid> }`.
  - `POST /api/projects {dir}` → registra; 200 `{pid, name}`; emite `projects`.
  - `DELETE /api/projects/{pid}` → remove; emite `projects`.
  - Rotas de dados escopadas `GET/POST/PUT /api/projects/{pid}/...` reusando os handlers atuais (espelha `multiproject.go`, mas resolvendo do **registry** e **sem** o middleware de segredo) + `GET /api/projects/{pid}/stream`? Não — **um** `GET /api/stream` global multiplexado basta.
  - SPA embarcada como hoje.
- **Construtor**: `NewUIMulti(ctx, resolver, pullInterval) (http.Handler, *registry)` — o `cmd` registra o projeto inicial e injeta o `resolver(dir) → (repoDir, projDir, projectID, name)`.

### `internal/cli/uicmd.go` — host vs cliente
- Tenta `net.Listen` na porta. **Sucesso → host**: cria o servidor multi, registra o projeto do cwd, abre o browser, serve; Ctrl+C encerra (shutdown gracioso do 0031).
- **`EADDRINUSE` → cliente**: faz `GET http://localhost:<port>/api/projects` para **confirmar** que é um cerne ui (forma/ça do payload); se não for, erro pt-BR claro. Senão `POST /api/projects {dir=cwd}`, imprime "projeto <nome> adicionado ao cerne ui em :<porta> (Ctrl+C para remover)", e **bloqueia** em `signal.NotifyContext`; no sinal, `DELETE /api/projects/{pid}` (best-effort) e sai.
- O `resolver` injetado no host = `resolveProject(dir)` (já existe), devolvendo `(repoDir, projDir, projectID)` + `name` (de `store.LoadProject`).

### `ui/` — seletor global + badge
- **`api.ts`**: base por projeto — todas as chamadas viram `/api/projects/${pid}/...`; `pid` vem do projeto ativo. `fetchProjects()` (lista + default). `subscribeChanges` passa a entregar `(pid)` por evento e a tratar o evento `projects`.
- **`projects.tsx`** (novo ou estende `live.tsx`): `ProjectProvider` com `{ projects, activePid, setActivePid, changed: Set<pid>, version }`. Regras do SSE: `change(pid)` → se `pid===activePid` `version++` (re-busca); senão `changed.add(pid)`. `projects` → re-busca a lista. `activePid` em `localStorage` (+ listener de `storage` p/ sincronizar abas); default = ativo salvo se ainda existir, senão o `default`. `useActiveProject()` / `useLiveVersion()`.
- **`Nav.tsx`**: à direita, `ProjectSelector` — 1 projeto → `<span>` com o nome; N → `<select>` (opções com badge "•" quando em `changed`). Trocar limpa o badge daquele pid e re-busca.
- As telas: nenhuma muda de lógica além de a base de fetch já levar o `pid` (centralizado no `api.ts` lendo o ativo do provider) e `version` continuar nas deps (0031).

---

## Verificação (TDD)
- **`internal/server`:** `TestRegistryAddRemove` (add resolve e sobe watcher; remove cancela); `TestProjectsListEndpoint` (GET lista + default); `TestRegisterEndpointAddsProject` (POST {dir} → 200 {pid,name} + projeto servível); `TestDeregisterEndpointRemoves` (DELETE → some da lista, watcher parado); `TestStreamTagsChangeWithPid` (commit no projeto X → `change` com `data: <pid> …`); `TestStreamEmitsProjectsOnRegister` (POST → evento `projects`). Reusa os fixtures `newGitBackend*` do 0031.
- **`internal/cli`:** `TestUIClientRegistersWhenPortBusy` (com um host de teste via `httptest`, o caminho cliente faz o POST e o DELETE); `TestUIClientRejectsNonCerneOnPort` (porta ocupada por outro handler → erro claro, sem registrar).
- **`ui/`:** build/typecheck (`npm run build`); `dist` reembutido. Verificação visual no `result.md`.
- `go vet ./...` + suíte verdes; `go build ./cmd/cerne` ok; verificação manual (A host + B join; seletor; badge; Ctrl+C remove B).

## Fora de escopo
- **Unificar com `cerne serve`** (host remoto, segredo S2S, clone-por-URL) — modelos distintos; possível refator futuro de código compartilhado.
- **`pid` na URL / deep-link por projeto** — o dono escolheu seleção global; fica como evolução se surgir a necessidade de duas abas em projetos diferentes.
- **Heartbeat / GC de cliente morto** — um cliente `kill -9` (sem Ctrl+C) deixa entrada órfã até o host reiniciar; deregister cobre o caminho gracioso. Heartbeat é follow-up.
- **Failover quando o host morre** — clientes atados ficam sem servidor; re-subir restabelece. Sem promoção automática de host.
- **Remoção pela UI** (botão "fechar projeto") — o ciclo é por processo (Ctrl+C); botão é follow-up.
- **Auth/multiusuário** — é `localhost` de dev.
