# Case 0033 — Resultado: tabela de rotas única

tags: #result #cerne #server #refactor #dx

**Status:** Implementado — suíte verde. Refatoração comportamento-preservante.
**Entregue:** uma fonte única (`dataRoutes()` + `mountDataRoutes`) para a API de dados, reusada pelos três servidores (`newMux`, `NewUIMulti`, `MultiServer`). Nenhuma rota nova; o subset do `cerne serve` preservado por uma flag.

---

## O que mudou

- **[internal/server/routes.go](../../../../internal/server/routes.go)** (novo): `scopedHandler` (handler ligado a um projeto resolvido), `projectBinder` (resolve o projeto da request), `dataRoute{method, pattern, remote, handler}`, `dataRoutes()` (as 21 rotas, uma vez só) e `mountDataRoutes(mux, prefix, bind, remoteOnly)`.
- **[server.go](../../../../internal/server/server.go)** `newMux`: ~63 linhas de `mux.HandleFunc` → um `bind` fixo + `mountDataRoutes(mux, "", bind, false)`. Stream/404/SPA intactos.
- **[uimulti.go](../../../../internal/server/uimulti.go)** `NewUIMulti`: ~21 rotas inline → `mountDataRoutes(mux, "/projects/{pid}", withProject, false)`. Endpoints de projetos, SSE, 404, SPA intactos.
- **[multiproject.go](../../../../internal/server/multiproject.go)** `MultiServer`: ~14 rotas inline → `mountDataRoutes(mux, "/projects/{pid}", withProject, true)`. `PUT` register, 404 e o segredo S2S intactos.

Os três `withProject`/`bind` (resolução de projeto, que **difere**) seguem locais a cada servidor; só a tabela rota→handler virou compartilhada.

## Aceitação

- `go vet ./...` limpo; **`go test ./...` = 523 testes verdes** (521 + 2 novos invariantes). *Os 521 existentes passaram sem edição* — prova de que o comportamento foi preservado.
- Novos testes: `TestServeExposesOnlyRemoteSubset` (serve: `/graph` → 404, `/state` → 200) e `TestUIMultiExposesFullRouteSet` (ui: `/graph` → 200). Travam o contrato do subset contra drift.
- `go build ./cmd/cerne` ok (sem mudança de UI/`dist`).

## Efeito
- Adicionar/renomear uma rota agora é **uma entrada** em `dataRoutes()` (com a flag `remote` decidindo se o `cerne serve` a expõe). Some o risco de drift entre as três tabelas que motivou o caso.

## Fora de escopo / follow-up
- **Caminho live single-project morto** (`NewLive`/`liveHub`/`watchHead`/`liveHub.handleStream`): sem chamadores desde o 0032; dead-code legítimo, removível num caso à parte (cascateia em `live.go`/`live_test.go`; `pullLoop`+constantes seguem usados pelo `uimulti`). Sinalizado.
- **Paridade total do `cerne serve`**: virar as 7 rotas `remote:false` em `true` é trivial se/quando o host precisar — decisão de produto, não feita aqui.

## Próximos passos
- Registrar no cerne: `cerne import` + `cerne pull`; commit `docs.lock`. Entrega: push + MR (`-R rockt/cerne`).
