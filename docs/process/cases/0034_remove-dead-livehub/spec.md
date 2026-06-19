# Case 0034 — Remover o caminho live single-project morto (`NewLive`/`liveHub`)

tags: #spec #cerne #server #refactor #cleanup

**Status:** Rascunho (2026-06-19) — spec para revisão; `plan.md`/`result.md` a seguir.
**Tipo:** Remoção de dead-code **sem mudança de comportamento**. Nenhuma rota, resposta ou fluxo de usuário muda.
**Origem:** Follow-up explícito sinalizado no `result.md` (e na seção "Fora de escopo") do caso 0033: o caminho de live-refresh single-project ficou sem chamadores desde o 0032, que migrou `cerne ui` para o `multiHub` multiplexado.

---

## Problema

Desde o caso 0032 (`cerne ui` multi-projeto), o `cerne ui` usa `server.NewUIMulti` ([uimulti.go](../../../../internal/server/uimulti.go)) com o `multiHub`. O construtor live single-project anterior — `server.NewLive` (caso 0031) e o `liveHub` que ele orquestra — **não tem nenhum chamador** no código de produção:

- `server.NewLive` ([server.go](../../../../internal/server/server.go)): o único caller histórico era `uicmd.go`, que migrou para `NewUIMulti` no 0032. `grep -rn "NewLive(" --include=*.go` mostra só a definição.
- `liveHub` + métodos `subscribe`/`unsubscribe`/`broadcast`/`watchHead`/`handleStream` ([live.go](../../../../internal/server/live.go)): só alcançáveis via `NewLive` (morto) e `live_test.go`. O servidor multi-projeto usa `multiHub` + `watchHeadMulti` no lugar.
- `newMux` carrega um parâmetro `hub *liveHub` que **sempre** recebe `nil` (só `New`, test-only, o chama) e um ramo `if hub != nil { /api/stream }` que nunca dispara.

É dead-code legítimo, mas a limpeza **cascateia** em `live.go` e `live_test.go` e precisa preservar o que segue vivo.

## Objetivo e critérios de sucesso

1. **Remover o dead-code:** `NewLive`, o tipo `liveHub` e seus métodos (`subscribe`/`unsubscribe`/`broadcast`/`watchHead`/`handleStream`), `newLiveHub`.
2. **Simplificar `newMux`:** dropar o parâmetro `hub *liveHub` e o ramo `if hub != nil { /api/stream }`. `New` (único caller, test-only) passa a chamar `newMux(repoDir, projDir, projectID)`.
3. **Preservar o que segue vivo:** `pullLoop`, `headPollInterval`, `keepaliveInterval` continuam em `live.go` — o `uimulti.go` os usa.
4. **Cobertura preservada:** `pullLoop` continua testado. `TestPullLoopAdvancesHeadFromRemote` é reescrito contra o `multiHub`/`watchHeadMulti` (o caminho vivo) em vez do `liveHub` morto.
5. **Sem regressão:** `go vet ./...` + `go test ./... -count=1` + `go build ./cmd/cerne` verdes. Sem mudança de UI/`dist`.

## Fora de escopo

- **Tocar `multiHub`/`watchHeadMulti`/`NewUIMulti`** (o caminho vivo): só são *referenciados* pelo teste reescrito, não alterados.
- **Mover `pullLoop`/constantes para fora de `live.go`:** seguem onde estão (usados pelo `uimulti`); mover seria churn sem ganho.
- **Unificar SSE entre `ui` e um futuro `serve` ao vivo:** não há caminho live no `serve`; nada a unificar.

## Desenho

### `internal/server/live.go`
Reduz a: `headPollInterval`, `keepaliveInterval`, `pullLoop`. Removidos: `liveHub` (tipo), `newLiveHub`, `subscribe`, `unsubscribe`, `broadcast`, `watchHead`, `handleStream`. Imports `fmt`/`net/http`/`sync` saem; `context`/`time`/`gitback` ficam (usados por `pullLoop`).

### `internal/server/server.go`
`NewLive` removido. `newMux` perde o parâmetro `hub` e o ramo do stream. `New` → `newMux(repoDir, projDir, projectID)`. Imports `context`/`time` saem (só `NewLive` os usava). Docstrings de `New`/`newMux` atualizadas: o stream vive só no handler multi-projeto (`NewUIMulti`).

### `internal/server/uimulti.go`
Só o docstring de `watchHeadMulti` ("Mirrors `liveHub.watchHead`…") perde a referência ao método deletado.

### `internal/server/live_test.go`
Removidos os 5 testes do caminho morto (`TestHubBroadcastsToSubscribers`, `TestHubUnsubscribe`, `TestStreamSendsCurrentShaOnConnect`, `TestStreamEmitsChangeOnHeadChange`, `TestStreamUnsubscribesOnClientCancel`) e os helpers órfãos que tocam `*liveHub` (`subCount`, `openStream`) + o `waitFor` (só o teste removido o usava). Mantidos `newGitBackendNoClean`, `readSSEData`, `waitEvent` — o `uimulti_test.go` os usa. `TestPullLoopAdvancesHeadFromRemote` reescrito contra `multiHub`+`watchHeadMulti`.

## Verificação (TDD)

- `TestPullLoopAdvancesHeadFromRemote` (reescrito): um segundo clone do origin dá commit+push (colega); com `watchHeadMulti`+`pullLoop` de intervalo curto no primeiro clone, o assinante do `multiHub` recebe `liveEvent{kind:"change", pid, sha}` com o HEAD remoto. Mantém `pullLoop` coberto **pelo caminho vivo**.
- Suíte existente cobre o resto; deve ficar verde sem mais edição (a remoção é dead-code).
- `go vet ./...` + `go build ./cmd/cerne` ok.
