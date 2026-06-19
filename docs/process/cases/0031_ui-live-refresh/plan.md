# Case 0031 — Plano de implementação

Commits TDD (teste vermelho → impl mínima → verde → commit), uma preocupação por commit. Suíte verde a cada commit (`go vet ./... && go test ./... -count=1`). Strings de usuário pt-BR; código/identificadores em inglês. Conventional Commits; corpo termina com `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

Backend primeiro (hub → stream → pull → wiring no `ui` cmd), depois o front (uma conexão, contador de versão, telas), por fim rebuild do `ui/dist`. Sem mudança de DTO; `server.New` preservado para os testes existentes — o ciclo de vida ao vivo entra por um construtor novo `NewLive`.

---

## 1. plan (registro)
- Espelho deste `plan.md` na pasta do caso + linha no índice `docs/process/CLAUDE.md`.
- `docs(cerne): case 0031 — ui live refresh (plan)`

## 2. server — `liveHub` (broadcaster SSE)
- **Teste:** `TestHubBroadcastsToSubscribers` (2 assinantes recebem o mesmo sha; canal cheio não bloqueia o `broadcast`); `TestHubUnsubscribe` (após `unsubscribe`, o canal sai do conjunto).
- **Impl:** `internal/server/live.go` — `liveHub{mu, subs map[chan string]struct{}}`, `newLiveHub`, `subscribe() chan string` (buffer 1), `unsubscribe(ch)` (delete+close sob lock), `broadcast(sha)` (send não-bloqueante: `select { case ch<-sha: default: }`).
- `feat(server): add liveHub SSE broadcaster`

## 3. server — observador de HEAD + `GET /api/stream` + `NewLive`
- **Teste (`httptest.NewServer`):** `TestStreamSendsCurrentShaOnConnect` (primeiro frame = HEAD atual); `TestStreamEmitsChangeOnHeadChange` (após um commit no clone, chega `event: change` com o novo sha); `TestStreamUnsubscribesOnClientCancel` (cancelar o ctx do request zera `len(hub.subs)`).
- **Impl:**
  - `watchHead(ctx, repoDir, interval)` — poll de `gitback.HeadSHA`; `broadcast` quando o sha avança; encerra no `ctx.Done()`.
  - `handleStream(w, r, repoDir)` — headers `text/event-stream`; `http.Flusher`; manda o sha atual no connect (ressync); `select` entre novo sha / keep-alive (`: \n\n` a cada 25s) / `r.Context().Done()`; `unsubscribe` no `defer`.
  - Refactor: corpo de `New` → `newMux(repoDir, projDir, projectID string, hub *liveHub)` que registra `GET /api/stream` **se** `hub != nil`. `New(...)` = `newMux(..., nil)` (sem stream, sem goroutine — testes atuais intactos). `NewLive(ctx, repoDir, projDir, projectID, pullInterval)` cria o hub, sobe `watchHead` (e o pull loop do passo 4), devolve `newMux(..., hub)`.
- `feat(server): stream backend changes over GET /api/stream (SSE + HEAD watcher)`

## 4. server — pull periódico do remoto
- **Teste:** `TestPullLoopAdvancesHeadFromRemote` — segundo clone do mesmo origin dá commit+push (colega); com `watchHead`+`pullLoop` de intervalo curto no primeiro clone, um assinante recebe o novo sha; `pull-interval` 0 → `NewLive` não sobe o loop.
- **Impl:** `pullLoop(ctx, repoDir, interval)` — `gitback.Pull` a cada tick (erro ignorado: offline não derruba o painel); só é iniciado por `NewLive` quando `interval > 0`. Notificação sai do `watchHead` (o pull só move o HEAD).
- `feat(server): periodically pull origin so remote pushes reach the UI`

## 5. cli — `cerne ui --pull-interval` + shutdown gracioso
- **Teste:** `TestUICmdPullIntervalFlag` (flag existe, default 15s, parseia `0`/durations).
- **Impl:** `uicmd.go` — flag `--pull-interval` (`DurationVar`, default `15s`, ajuda pt-BR "0 desliga"); `signal.NotifyContext(…, os.Interrupt)`; servir via `http.Server{Handler: server.NewLive(sigCtx, …, pullInterval)}` + `Shutdown` no `sigCtx.Done()`; `errors.Is(err, http.ErrServerClosed)` → nil.
- `feat(cli): cerne ui auto-refreshes; --pull-interval syncs the remote (0 disables)`

## 6. ui — uma conexão ao vivo + contador de versão
- **Impl:**
  - `ui/src/api.ts`: `subscribeChanges(onChange: () => void): () => void` — abre `EventSource('/api/stream')`, chama `onChange` no evento `change`; devolve um disposer.
  - `ui/src/live.tsx`: `LiveProvider` (abre a inscrição uma vez, incrementa `version`) + `useLiveVersion(): number` via contexto.
  - `ui/src/App.tsx`: envolve `<Routed>` em `<LiveProvider>`.
- `feat(ui): live update channel (EventSource → version counter)`

## 7. ui — telas re-buscam ao mudar a versão
- **Impl:** nas telas de estado (Kanban, FeaturePage, Decisions lista+detalhe, References lista+detalhe, Lint, History, Graph): `const v = useLiveVersion()` e `v` nas deps do `useEffect` de fetch. **Sem flicker**: não zerar o estado no re-fetch (o padrão atual já só faz `.then(set…)`; preservar). Search/Ask ficam de fora (disparam por input).
- `feat(ui): refetch state views when the backend changes`

## 8. Rebuild do bundle
- `cd ui && npm ci && npm run build`; commitar `ui/dist`.
- `build(ui): rebuild dist with live refresh`

## 9. Fechamento
- `go vet ./...` + `go test ./... -count=1` + `go build ./cmd/cerne` verdes; verificação manual (`cerne ui`, mutação por CLI noutro terminal → tela atualiza sem F5; push remoto → atualiza em ≤ `pull-interval`).
- `result.md` (números de aceite, débitos, próximos passos) + índice `docs/process/CLAUDE.md` → Implemented.
- Registrar no cerne: `cerne import` + `cerne pull`; commit do `docs.lock`. Entrega: push + MR (`-R rockt/cerne`).

## Riscos / notas
- **Edição em andamento (DocEditor):** o reset só dispara se o `content` daquele doc mudar de fato (strings iguais não disparam o `useEffect`); um refresh externo durante a edição preserva o rascunho a menos que aquele doc tenha mudado no backend. Aceitável; anotar no `result.md`.
- **Custo do poll de HEAD:** uma leitura de ref do git por segundo por processo `ui`; desprezível. Otimização futura (stat de `.git/HEAD`) fora de escopo.
- **Multiprojeto (`serve`):** fora de escopo; o `watchHead` por-projeto com registro dinâmico fica como follow-up.
