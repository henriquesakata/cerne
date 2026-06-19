# Case 0033 — Plano de implementação

Refatoração comportamento-preservante. Conventional Commits; corpo termina com `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`. A suíte existente (521) é a rede de segurança: deve ficar verde **sem edição** dos testes atuais.

---

## 1. plan (registro)
- Espelho deste `plan.md` + linha no índice `docs/process/CLAUDE.md`.
- `docs(cerne): case 0033 — unify route tables (plan)`

## 2. routes.go — fonte única
- **Teste:** `TestServeExposesOnlyRemoteSubset` (novo): `MultiServer` + projeto registrado → `/api/projects/{pid}/graph` 404, `/state` 200; `NewUIMulti` → ambos 200. (Vermelho não aplica — a tabela ainda não existe; o teste passa a valer junto com o passo 3.)
- **Impl:** `internal/server/routes.go` — `scopedHandler`, `projectBinder`, `dataRoute{method,pattern,remote,handler}`, `dataRoutes()` (21 rotas; `remote:true` em state, features/{id} GET, POST features, decisions/{id}, references/{id}, POST references, search, ask, config/llm GET+PUT, wiki, wiki/{rel...}, events, lint; `remote:false` em transition, diagram, docs PUT, parent PUT, artifacts GET+PUT, graph), `mountDataRoutes(mux, prefix, bind, remoteOnly)`.
- `refactor(server): extract dataRoutes() as the single source of truth`

## 3. Reescrever os três construtores para usar `mountDataRoutes`
- **server.go `newMux`**: `bind` fixo → `mountDataRoutes(mux,"",bind,false)`; mantém stream(if hub)/404/SPA.
- **uimulti.go `NewUIMulti`**: passa o `withProject` (já é `projectBinder`) → `mountDataRoutes(mux,"/projects/{pid}",withProject,false)`; remove as ~21 linhas inline; mantém endpoints de projetos/stream/404/SPA.
- **multiproject.go `MultiServer.mux`**: passa o `withProject` → `mountDataRoutes(mux,"/projects/{pid}",withProject,true)`; remove as ~14 linhas inline; mantém PUT register/404; segredo intacto.
- **Verificação:** `go vet` + `go test ./... -count=1` (521 + 1 novo) verdes; `go build ./cmd/cerne` ok.
- `refactor(server): mount the shared route table in newMux/uimulti/multiproject`

## 4. Fechamento
- `result.md` + índice → Implemented. Registrar no cerne (`import`+`pull`, commit `docs.lock`). Push + MR (`-R rockt/cerne`).
- Follow-up sinalizado: remover o caminho live single-project morto (`NewLive`/`liveHub`).

## Notas / riscos
- **Paridade exata do subset**: as 14 `remote:true` devem casar 1:1 com o que o `multiproject.go` expõe hoje; o teste novo + a suíte de `multiproject_test.go` travam isso.
- **`projectBinder` calça nos três**: o `withProject` do `ui` e do `serve` já têm a assinatura `func(scopedHandler) http.HandlerFunc`; o single-project cria um trivial.
- **Padrões do ServeMux**: mesmos literais de antes (`{id}`, `{rel...}`, dois wildcards em artifacts) — só movidos para a tabela.
