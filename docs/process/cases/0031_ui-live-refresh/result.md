# Case 0031 — Resultado: UI ao vivo (a tela atualiza sozinha)

tags: #result #cerne #ui #sse #dx #live-reload

**Status:** Implementado — suíte verde; UI rebuildada. Aguardando revisão + verificação manual end-to-end no navegador.
**Entregue:** auto-refresh do `cerne ui` via SSE (`GET /api/stream`) com observador do HEAD do backend e pull periódico opcional do remoto. Sem mudança de DTO; `server.New` preservado.

---

## O que shippou

### Backend (Go)
- **[internal/server/live.go](../../../../internal/server/live.go)** (novo):
  - `liveHub` — broadcaster SSE thread-safe (`subscribe`/`unsubscribe`/`broadcast` não-bloqueante; buffer 1 por assinante).
  - `watchHead(ctx, repoDir, interval)` — poll de `gitback.HeadSHA` (1s em produção); `broadcast` quando o HEAD avança. Cobre **toda** mutação (lifecycle, doc, artifact, referência — todas viram commit) e o efeito de um pull.
  - `pullLoop(ctx, repoDir, interval)` — `gitback.Pull` periódico; erro ignorado (offline não derruba o painel). Não fala com o hub: o pull só move o HEAD, e o `watchHead` percebe — **um caminho único** de notificação.
  - `handleStream` — endpoint `text/event-stream`: manda o SHA atual no connect (ressync no reconnect), `event: change` a cada avanço, keep-alive a cada 25s, `unsubscribe` no disconnect.
- **[internal/server/server.go](../../../../internal/server/server.go)** — `New` preservado (`= newMux(..., nil)`, sem stream/goroutine → testes antigos intactos); novo `NewLive(ctx, repoDir, projDir, projectID, pullInterval)` sobe observador + (se `>0`) pull e registra `GET /api/stream`.
- **[internal/cli/uicmd.go](../../../../internal/cli/uicmd.go)** — flag `--pull-interval` (default `15s`, `0` desliga → offline-by-default); `signal.NotifyContext(os.Interrupt)` + `http.Server.Shutdown` gracioso (goroutines não sobrevivem ao listener).

### Front (React/TS)
- **[ui/src/live.tsx](../../../../ui/src/live.tsx)** (novo) — `LiveProvider` (uma conexão SSE p/ todo o app) + `useLiveVersion()` (contador que incrementa a cada `change`).
- **[ui/src/api.ts](../../../../ui/src/api.ts)** — `subscribeChanges(onChange)` encapsula o `EventSource('/api/stream')`.
- **[ui/src/App.tsx](../../../../ui/src/App.tsx)** — envolve a árvore no `<LiveProvider>`.
- **Telas religadas ao contador** (re-buscam ao vivo, limpam erro no sucesso, sem zerar o estado → **sem flicker**): Kanban, FeaturePage, Decisions (lista+detalhe), References (lista+detalhe), Lint, History, Graph. Search/Ask ficam de fora (disparam por input).

## Aceitação

- `go vet ./...` limpo; **`go test ./...` = 513 testes verdes** (506 + 7 novos: 6 em `internal/server` — hub broadcast/unsubscribe, stream connect/change/cancel, pull-loop remoto — e 1 em `internal/cli` — flag `--pull-interval`). Os testes do stream que dirigem goroutines usam base temporária com limpeza best-effort (handle do go-git no Windows).
- `go build ./...` ok (re-embed do `ui/dist`); `cerne ui --help` mostra `--pull-interval duration … (default 15s)`.
- `npm ci && npm run build` ok (`✓ built in ~30s`); `ui/dist` rebuildado e commitado.
- Cobertura end-to-end do mecanismo SSE nos testes via `httptest` (frames reais lidos pela conexão, commit real avançando o HEAD, pull real trazendo um commit “remoto”).

## Verificação manual recomendada (precisa do backend real)
1. `cerne ui` num terminal; em outro, `cerne feature approve F-XXXX` (ou edite um doc pela UI noutra aba) → o kanban/página reflete em ≤ ~2s **sem F5**.
2. Push de outro autor no backend → reflete em ≤ `--pull-interval` (default 15s).
3. `cerne ui --pull-interval 0` → sem pull periódico (só mudanças locais), restaurando o offline estrito.

## Débitos / limitações (conhecidos)
- **Edição em andamento (DocEditor):** um refresh externo durante a edição preserva o rascunho **a menos que** aquele doc específico tenha mudado no backend (strings iguais não disparam o reset). Aceitável; se incomodar, follow-up: pausar o refresh enquanto há edição ativa.
- **Multiprojeto (`cerne serve`)** não tem auto-refresh — fora de escopo (registro dinâmico de N backends). Follow-up.
- **Custo do poll de HEAD:** uma leitura de ref/s por processo `ui` (desprezível). Otimização futura possível (stat de `.git/HEAD`).
- **Sem indicador visual** de “atualizado” — a atualização é silenciosa. Follow-up se confundir.

## Próximos passos
- Registrar no cerne: `cerne import` + `cerne pull`; commit do `docs.lock`.
- Entrega: push + MR no `rockt/cerne` (`-R rockt/cerne` no glab).
