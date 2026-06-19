# Case 0032 — Plano de implementação

Commits TDD (teste vermelho → impl mínima → verde → commit), uma preocupação por commit. Suíte verde a cada commit (`go vet ./... && go test ./... -count=1`). Strings de usuário pt-BR; código/identificadores em inglês. Conventional Commits; corpo termina com `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

Backend (registry + SSE multiplexado + rotas escopadas) → CLI (host/cliente por detecção de porta) → front (base por pid + provider + seletor) → rebuild do `dist`. `server.New`/`NewLive` (single-project) **preservados** — o modo multi entra por um construtor novo.

---

## 1. plan (registro)
- Espelho deste `plan.md` na pasta do caso + linha no índice `docs/process/CLAUDE.md`.
- `docs(cerne): case 0032 — ui multi-project (plan)`

## 2. server — `multiHub` (SSE multiplexado por projeto)
- **Teste:** `TestMultiHubBroadcastsChangeWithPid` (assinante recebe `{change, pid, sha}`); `TestMultiHubBroadcastsProjects`; broadcast não-bloqueante; unsubscribe.
- **Impl:** `internal/server/uimulti.go` — `liveEvent{kind, pid, sha}`, `multiHub{subs map[chan liveEvent]struct{}}`, `subscribe/unsubscribe/broadcast`. `handleStream` emite `event: change\ndata: <pid> <sha>` e `event: projects\ndata:`; connect manda `projects`; keep-alive 25s; unsubscribe no `r.Context().Done()`.
- `feat(server): multiplexed SSE hub (change-with-pid + projects events)`

## 3. server — registry de projetos + watcher por projeto
- **Teste:** `TestRegistryAddStartsWatcher` (add resolve via Resolver fake, sobe watchHead; commit no repo → `change` com o pid); `TestRegistryRemoveStopsWatcher` (cancel + some da lista); `TestRegistryAddIdempotent` (mesmo pid não duplica).
- **Impl:** `Resolver func(dir) (repoDir, projDir, projectID, name string, err error)`; `registry{mu, projects map[pid]*entry{name,repoDir,projDir,cancel}, order []pid, hub, resolve, parent ctx, pull}`; `add(dir)` resolve, deriva ctx do parent, sobe `watchHeadMulti(ctx, hub, pid, repoDir, headPollInterval)` (+ `pullLoop` do 0031 se `pull>0`), emite `projects`; `remove(pid)` cancela+emite; `list() ([]entry, default)`.
- `feat(server): per-project registry with live-refresh watchers`

## 4. server — rotas escopadas + endpoints de projetos + `NewUIMulti`
- **Teste:** `TestProjectsListEndpoint` (GET /api/projects → lista+default); `TestRegisterEndpoint` (POST {dir} → 200 {pid,name}, servível em /api/projects/{pid}/state); `TestDeregisterEndpoint` (DELETE → 404 depois); `TestScopedStateRoute` (GET /api/projects/{pid}/state devolve o estado certo); `TestUnknownProject404`.
- **Impl:** `NewUIMulti(ctx, resolve, pullInterval, initialDir) (http.Handler, error)` — cria hub+registry, registra `initialDir`, monta: `GET/POST /api/projects`, `DELETE /api/projects/{pid}`, `GET /api/stream`, e as rotas de dados escopadas `/api/projects/{pid}/...` (paridade **total** com `newMux`: state, features CRUD+transition+diagram+docs+parent, artifacts, decisions, references, search, ask, wiki, config/llm, lint, graph, events) via `withProject` resolvendo do registry; SPA fallback. (Tabela de rotas espelha `newMux`; duplicação isolada — unificar as 3 tabelas é follow-up.)
- `feat(server): NewUIMulti — project-scoped routes + register/deregister`

## 5. cli — `cerne ui` host vs cliente (join por porta ocupada)
- **Teste:** `TestJoinRegistersAndDeregisters` (host de teste via `httptest`; `registerProject`/`deregisterProject` batem nos endpoints); `TestProbeRejectsNonCerne` (porta com outro handler → erro claro); `TestUICmdFlagsUnchanged` (pull-interval ainda presente).
- **Impl:** em `uicmd.go`: tenta `net.Listen`; sucesso → **host** (`server.NewUIMulti(sigCtx, resolver, pull, startDir)`, serve, shutdown gracioso do 0031); `EADDRINUSE` (`errors.Is(err, syscall.EADDRINUSE)` + fallback de string) → **cliente** (`joinRunningUI`): `probeCerneUI(base)` (GET /api/projects confere o shape) → `registerProject(base, absDir)` → imprime + bloqueia em `signal.NotifyContext` → no sinal `deregisterProject(base, pid)`. `resolver` = `resolveProject(dir)` + `store.LoadProject` p/ o nome. Cliente envia **dir absoluto** (host resolve).
- `feat(cli): cerne ui joins a running instance when the port is in use`

## 6. ui — base por projeto + provider + seletor
- **Impl:**
  - `api.ts`: `apiBase()` = `/api/projects/${activePid}`; prefixar **todas** as chamadas de dados; `getApiProject/setApiProject`; `fetchProjects()` (GET /api/projects); `subscribeLive({onChange(pid), onProjects})` (parseia `change`/`projects`).
  - `projects.tsx` (substitui `live.tsx`): `ProjectProvider` carrega a lista, escolhe o ativo (localStorage ∈ lista, senão default), `setApiProject`, assina o SSE (`change pid`: ativo→`version++`, senão `changed.add(pid)`; `projects`→recarrega lista), sincroniza abas via evento `storage`. Hooks: `useLiveVersion()` (telas, inalteradas) + `useProjects()` (lista, ativo, setAtivo, changed).
  - `Nav.tsx`: à direita, `ProjectSelector` — 1 projeto → nome; N → `<select>` com badge "•" nos que estão em `changed`; trocar limpa o badge + `version++`.
  - `App.tsx`: `ProjectProvider` no topo; não renderiza as telas até ter o ativo (evita fetch sem pid).
- `feat(ui): project selector + per-project API base (multi-project ui)`

## 7. Rebuild do bundle
- `cd ui && npm ci && npm run build`; commitar `ui/dist`.
- `build(ui): rebuild dist with the project selector`

## 8. Fechamento
- `go vet ./...` + `go test ./... -count=1` + `go build ./cmd/cerne` verdes; verificação manual (A host + B join → seletor; trocar projeto; badge no não-ativo; Ctrl+C no B remove).
- `result.md` + índice `docs/process/CLAUDE.md` → Implemented.
- Registrar no cerne: `cerne import` + `cerne pull`; commit `docs.lock`. Entrega: push + MR (`-R rockt/cerne`).

## Riscos / notas
- **Paridade de rotas:** a tabela escopada precisa cobrir tudo que o front usa (transition/diagram/docs/parent/artifacts/graph), não só o subset do `cerne serve`. Garantir no teste de fumaça.
- **Cliente morto (`kill -9`):** deixa entrada órfã até o host reiniciar (deregister só no Ctrl+C). Heartbeat = follow-up.
- **`EADDRINUSE` no Windows:** validar `errors.Is(err, syscall.EADDRINUSE)`; manter fallback de string ("Only one usage of each socket address").
