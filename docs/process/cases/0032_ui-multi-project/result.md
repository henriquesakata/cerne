# Case 0032 — Resultado: `cerne ui` multi-projeto

tags: #result #cerne #ui #multiprojeto #sse #dx

**Status:** Implementado — suíte verde; UI rebuildada. Aguardando revisão + verificação manual no navegador.
**Entregue:** um `cerne ui` serve N projetos. O 1º vira host (porta :7717); rodar `cerne ui` em outro projeto faz **join** no host (registra e fica atado, removendo no Ctrl+C). Seletor de projeto global no header, com badge de mudança nos não-ativos.

---

## O que shippou

### Backend (Go)
- **[internal/server/uimulti.go](../../../../internal/server/uimulti.go)** (novo):
  - `multiHub` — SSE multiplexado: `liveEvent{kind, pid, sha}`, broadcast não-bloqueante; `handleStream` emite `event: change\ndata: <pid> <sha>` e `event: projects`, com resync no connect e keep-alive.
  - `registry` — `pid → {name, repoDir, projDir, cancel}`; `add(dir)` resolve via `Resolver` injetado, sobe `watchHeadMulti` (+ `pullLoop` do 0031 se intervalo > 0) num contexto próprio, emite `projects`; `remove(pid)` cancela e emite; `list()` (ordem de registro; default = primeiro). Idempotente por `projectID`.
  - `NewUIMulti(ctx, resolve, pullInterval, initialDir)` — registra o projeto inicial e monta: `GET/POST /api/projects`, `DELETE /api/projects/{pid}`, `GET /api/stream`, as rotas de dados escopadas `/api/projects/{pid}/...` (paridade total com o mux single-project, reusando os mesmos handlers) e a SPA.
- **`server.New`/`NewLive`** (single-project) **preservados** — testes e `cerne serve` intactos.

### CLI (Go)
- **[internal/cli/uicmd.go](../../../../internal/cli/uicmd.go)** — host vs cliente por detecção de porta: `net.Listen` OK → **host** (`NewUIMulti`, serve, shutdown gracioso); `EADDRINUSE` (`errors.Is(syscall.EADDRINUSE)` + fallback de string p/ Windows) → **cliente** (`joinRunningUI`): `probeCerneUI` confirma o `:7717`, `registerProject(dir absoluto)`, fica atado em `signal.NotifyContext`, e `deregisterProject` no Ctrl+C. `uiResolver` = `resolveProject` + `store.LoadProject` (nome).

### Front (React/TS)
- **[api.ts](../../../../ui/src/api.ts)** — `setApiProject(pid)` + `base()`: toda chamada de dados vira `/api/projects/{pid}/...`; `fetchProjects()` e `subscribeLive({onChange(pid), onProjects})` (rotas globais `/api/projects` e `/api/stream`).
- **[live.tsx](../../../../ui/src/live.tsx)** — `LiveProvider` carrega a lista, escolhe o ativo (localStorage ∈ lista, senão default), mantém `setApiProject` em sincronia (via `activeRef`, síncrono antes do refetch), assina o SSE (`change pid`: ativo→`version++`, senão badge; `projects`→recarrega), sincroniza abas via evento `storage`. `useLiveVersion()` (telas) + `useProjects()` (Nav).
- **[Nav.tsx](../../../../ui/src/Nav.tsx)** — `ProjectSelector` à direita: 1 projeto → nome; N → `select` com "•" nos que mudaram; trocar limpa o badge e re-busca.

## Aceitação

- `go vet ./...` limpo; **`go test ./...` = 520 testes verdes** (513 + 7 novos: 4 em `internal/server` — hub multiplexado, watcher por projeto, register/list/deregister, register de dir inválido — e 3 em `internal/cli` — probe aceita/rejeita, register+deregister).
- `go build ./...` ok (re-embed do `ui/dist`); `npm run build` ok; `ui/dist` rebuildado e commitado.

## Verificação manual recomendada
1. `cerne ui` no projeto A (host); em outro terminal, `cerne ui` no projeto B → o seletor de A passa a listar A e B (sem F5); o terminal de B fica atado ("Ctrl+C para remover").
2. Trocar o projeto no seletor → as telas mostram a doc do projeto escolhido.
3. Mexer no projeto **não-ativo** → "•" aparece nele no seletor; selecioná-lo limpa o badge e re-busca.
4. Ctrl+C no `cerne ui` de B → B some do seletor de A.
5. Porta ocupada por outro app → erro claro ("não parece um cerne ui").

## Débitos / limitações (conhecidos)
- **Cliente morto com `kill -9`** (sem Ctrl+C) deixa entrada órfã até o host reiniciar — só o caminho gracioso faz deregister. Heartbeat = follow-up.
- **Host morre** → clientes atados ficam sem servidor (sem promoção automática de host). Re-subir restabelece.
- **Tabela de rotas duplicada** entre `newMux`, `uimulti.go` e `multiproject.go` (3 cópias, handlers compartilhados). Unificar é follow-up.
- **`pid` não vai na URL** (seleção global, decisão do dono): duas abas não veem projetos diferentes ao mesmo tempo. Evolução possível.
- **Trocar de projeto numa página de detalhe** (`#/f/<id>`) pode 404 se o id não existir no novo projeto — voltar ao kanban resolve.

## Próximos passos
- Registrar no cerne: `cerne import` + `cerne pull`; commit `docs.lock`. Entrega: push + MR (`-R rockt/cerne`).
