# Case 0034 — Resultado: caminho live single-project morto removido

tags: #result #cerne #server #refactor #cleanup

**Status:** Implementado — suíte verde. Remoção de dead-code sem mudança de comportamento.
**Entregue:** `NewLive` e o tipo `liveHub` (com `subscribe`/`unsubscribe`/`broadcast`/`watchHead`/`handleStream` + `newLiveHub`) removidos; `newMux` perdeu o parâmetro `hub` e o ramo `/api/stream` que nunca disparava. O live-refresh do `cerne ui` segue inteiro pelo `multiHub`/`watchHeadMulti` (caso 0032).

---

## O que mudou

- **[internal/server/live.go](../../../../internal/server/live.go):** encolheu para o que o `uimulti` usa — `headPollInterval`, `keepaliveInterval`, `pullLoop`. Apagados o `liveHub` e seus 5 métodos + `newLiveHub`; imports `fmt`/`net/http`/`sync` saíram.
- **[internal/server/server.go](../../../../internal/server/server.go):** `NewLive` removido; `newMux(repoDir, projDir, projectID)` (sem `hub *liveHub`) sem o ramo `if hub != nil { /api/stream }`; `New` chama a forma nova; imports `context`/`time` saíram (só `NewLive` os usava). Docstrings de `New`/`newMux` agora apontam o stream para o handler multi-projeto (`NewUIMulti`).
- **[internal/server/uimulti.go](../../../../internal/server/uimulti.go):** docstring de `watchHeadMulti` perdeu a referência ao `liveHub.watchHead` deletado (o resto intacto — é o caminho vivo).
- **[internal/server/live_test.go](../../../../internal/server/live_test.go):** removidos os 5 testes do caminho morto (`TestHubBroadcastsToSubscribers`, `TestHubUnsubscribe`, `TestStreamSendsCurrentShaOnConnect`, `TestStreamEmitsChangeOnHeadChange`, `TestStreamUnsubscribesOnClientCancel`) e os helpers presos ao `*liveHub` (`subCount`, `openStream`) + o `waitFor` (exclusivo do teste removido). `TestPullLoopAdvancesHeadFromRemote` reescrito contra `multiHub`+`watchHeadMulti`. Mantidos `newGitBackendNoClean`/`readSSEData`/`waitEvent` (o `uimulti_test.go` os usa).

## Aceitação

- `go vet ./...` limpo; **`go test ./... = 518 testes verdes`** (523 do 0033 − 5 testes do caminho morto; o `TestPullLoopAdvancesHeadFromRemote` reescrito é net-zero). O resto da suíte passou sem edição — prova de que nada de comportamento mudou.
- `TestPullLoopAdvancesHeadFromRemote` (reescrito) mantém `pullLoop` coberto: um segundo clone empurra um commit; `watchHeadMulti`+`pullLoop` de intervalo curto fazem o assinante do `multiHub` receber `liveEvent{kind:"change", pid, sha}` com o HEAD remoto.
- `go build ./cmd/cerne` ok (sem mudança de UI/`dist`).

## Efeito

- Some uma API morta (`NewLive`) e um tipo inteiro (`liveHub`, ~95 linhas em `live.go`) que só existiam pré-0032. `live.go` fica com 3 itens, todos usados pelo `uimulti`. `newMux` perde um parâmetro e um ramo que nunca executava — menos superfície, mesma cobertura.

## Fora de escopo / follow-up

- **`multiHub`/`watchHeadMulti`/`NewUIMulti`** (caminho vivo) não foram tocados — só referenciados pelo teste reescrito.
- Nenhum débito novo aberto. `pullLoop` e as constantes ficam em `live.go`; movê-los para `uimulti.go` seria churn sem ganho (decisão consciente, não pendência).

## Próximos passos

- Registrar no cerne: `cerne import` + `cerne pull`; commit `docs.lock`. Entrega: merge do `main` recente, push + MR (`-R rockt/cerne`).
