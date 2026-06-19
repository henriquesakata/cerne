# Case 0033 — Unificar as tabelas de rotas do servidor (fonte única)

tags: #spec #cerne #server #refactor #dx

**Status:** Rascunho (2026-06-19) — spec para revisão; `plan.md`/`result.md` a seguir.
**Tipo:** Refatoração interna **sem mudança de comportamento**. Remove a duplicação da tabela de rotas da API entre os três servidores; nenhuma rota nova, nenhuma resposta diferente.
**Origem:** Débito anotado no `result.md` do caso 0032: três tabelas de rotas paralelas (`newMux`, `NewUIMulti`, `MultiServer`) que precisam ser mantidas em sincronia à mão — risco real de drift (o 0032 literalmente copiou a tabela do `newMux` para o `uimulti.go`).

---

## Problema

A mesma API de dados (`/api/state`, `/api/features/{id}`, …) é registrada em **três** lugares de `internal/server`, cada um com sua própria lista de `mux.HandleFunc`:

| Construtor | Uso | Prefixo | Cobertura | Auth |
|---|---|---|---|---|
| `New`/`newMux` ([server.go](../../../../internal/server/server.go)) | **testes** (`cerne ui` migrou p/ `NewUIMulti` no 0032) | `/api/…` | **completa** (21 rotas) | nenhuma |
| `NewUIMulti` ([uimulti.go](../../../../internal/server/uimulti.go)) | `cerne ui` (multi-projeto) | `/api/projects/{pid}/…` | **completa** (21) | nenhuma (localhost) |
| `NewMultiProject` ([multiproject.go](../../../../internal/server/multiproject.go)) | `cerne serve` (host remoto) | `/api/projects/{pid}/…` | **subset** (14) | segredo S2S |

As três chamam **os mesmos handlers** (`handleState`, `handleFeatureDetail`, …, todos já parametrizados por `repoDir`/`projDir`/`projectID`). Só o **mapeamento rota→handler** está triplicado. Adicionar/renomear uma rota hoje exige editar até três arquivos; esquecer um causa divergência silenciosa (ex.: uma rota nova no `ui` ausente no teste single-project).

O `serve` expõe deliberadamente um **subset** (sem `transition`, `diagram`, docs `PUT`, `parent` `PUT`, `artifacts`, `graph`) — escolha do caso 0022 (host remoto, menor superfície). Essa distinção precisa ser **preservada**.

## Objetivo e critérios de sucesso

1. **Fonte única.** Uma definição `dataRoutes()` (método, padrão, handler, flag `remote`) e um `mountDataRoutes` que os três construtores chamam. Adicionar uma rota = uma entrada na tabela.
2. **Zero mudança de comportamento.** As mesmas rotas, nos mesmos prefixos, com a mesma resolução de projeto e o mesmo subset no `serve` (via flag `remote`). `New`/`NewUIMulti` montam tudo; `MultiServer` monta só as `remote`.
3. **Resolução de projeto preservada.** Cada construtor injeta seu *binder* (single-project fixo; `ui` resolve do registry; `serve` resolve de `CacheDir` + checa registro) — a parte que **difere** continua local a cada um.
4. **Extras intactos.** `ui`/`serve` mantêm seus endpoints de projetos (`GET/POST /api/projects`, `DELETE`, `PUT` register), o SSE (`/api/stream` no `ui`), o 404 de `/api/` e a SPA — fora da tabela compartilhada.
5. **Invariante testada.** Novo teste fixa o contrato: `serve` responde 404 numa rota não-`remote` (ex.: `/graph`) e 200 numa `remote` (ex.: `/state`); o `ui` responde às duas. Trava o subset contra drift futuro.
6. **Sem regressão.** `go vet` + suíte (521) + `go build` verdes.

## Fora de escopo

- **Remover o caminho live single-project morto** (`NewLive`/`liveHub`/`watchHead`/`liveHub.handleStream`): sem chamadores desde o 0032 (`cerne ui` usa `multiHub`). É dead-code legítimo, mas é uma limpeza **distinta** (cascateia em `live.go`/`live_test.go`, e `pullLoop`+constantes seguem usados pelo `uimulti`). Vira follow-up.
- **Dar paridade total ao `cerne serve`**: manter o subset (flag `remote`). Expor as 7 rotas extras no host remoto é decisão de produto à parte — trivial depois (virar `remote: true`).
- **Unificar os *binders*** (resolução de projeto): a divergência é intencional (fixo vs registry vs cache+secret).

## Desenho

### `internal/server/routes.go` (novo)
- `type scopedHandler func(w, r, repoDir, projDir, projectID string)` — handler já ligado a um projeto resolvido.
- `type projectBinder func(scopedHandler) http.HandlerFunc` — resolve o projeto da request (fixo / por `{pid}`) e chama o handler.
- `type dataRoute struct { method, pattern string; remote bool; handler scopedHandler }`.
- `func dataRoutes() []dataRoute` — **a** tabela: as 21 rotas, com `remote: true` nas 14 que o `serve` expõe hoje.
- `func mountDataRoutes(mux *http.ServeMux, prefix string, bind projectBinder, remoteOnly bool)` — itera; pula `!remote` quando `remoteOnly`; registra `method+" /api"+prefix+pattern` com `bind(handler)`.

### Reescrita dos três (só o bloco de rotas)
- **`server.go` `newMux`**: `bind` fixo (fecha sobre `repoDir/projDir/projectID`) → `mountDataRoutes(mux, "", bind, false)`. Mantém `if hub != nil` stream, 404, SPA.
- **`uimulti.go` `NewUIMulti`**: o `withProject` (registry) já é um `projectBinder` → `mountDataRoutes(mux, "/projects/{pid}", withProject, false)`. Mantém endpoints de projetos, stream multiplexado, 404, SPA.
- **`multiproject.go` `MultiServer.mux`**: o `withProject` (cache + registered) já é um `projectBinder` → `mountDataRoutes(mux, "/projects/{pid}", withProject, true)`. Mantém `PUT /api/projects/{pid}`, 404; o segredo S2S segue envolvendo tudo.

## Verificação (TDD)
- **Novo** `internal/server`: `TestServeExposesOnlyRemoteSubset` — com `MultiServer` + projeto registrado, `/api/projects/{pid}/graph` → 404 e `/state` → 200; com `NewUIMulti`, `/graph` e `/state` → 200. (Trava o subset.)
- Suíte existente (521) cobre o comportamento das três rotas: deve **permanecer verde sem edição** (prova de que o refactor preserva).
- `go vet ./...` + `go build ./cmd/cerne` ok.
