# Case 0031 — UI ao vivo: a tela atualiza sozinha quando o backend muda

tags: #spec #cerne #ui #sse #dx #live-reload

**Status:** Rascunho (2026-06-19) — spec para revisão; `plan.md`/`result.md` a seguir.
**Tipo:** Evolutivo (DX). Adiciona atualização automática ao `cerne ui` sem mudar nenhum contrato de dados nem o modelo de fonte-da-verdade — só fecha a malha "backend mudou → tela reflete".
**Origem:** Dor de uso no dogfooding: hoje, para ver qualquer alteração no painel é preciso dar **F5**. Quem mexe na documentação por outro caminho (CLI em outro terminal, um agente trabalhando em background, outra aba do próprio UI, ou um colega que deu push no backend) não vê a mudança até recarregar a página à mão. Decisão de produto registrada com o dono: cobrir **mudanças locais e remotas**, via **SSE + observação do backend**, com **pull periódico** opcional.

---

## Problema

O `cerne ui` ([`internal/cli/uicmd.go`](../../../../internal/cli/uicmd.go)) serve uma API JSON + o front React embarcado. Duas características explicam o F5 obrigatório:

1. **O servidor já é stateless** — bom: cada endpoint lê o estado direto do disco (o clone de cache `repoDir`) a cada request, sem cache em memória ([`handleState`](../../../../internal/server/server.go) faz `store.LoadAllFeatures(repoDir)` etc. a cada chamada). Ou seja, o servidor **sempre** serve o estado fresco — falta só o front voltar a perguntar.

2. **O front busca uma única vez** — cada tela faz `fetch` no `useEffect(…, [])` (no mount) e nunca mais re-busca: [`Kanban.tsx`](../../../../ui/src/Kanban.tsx), `FeaturePage`, `Decisions`, `References`, `Lint`, `History`, `Graph`. Não há polling, `EventSource` nem `WebSocket` no código-fonte. O F5 é, na prática, um re-fetch manual.

Há ainda um segundo eixo, sobre **de onde** vêm as mudanças e se sequer chegam ao `repoDir` que o servidor lê:

| Origem da mudança | Chega ao `repoDir` lido pelo servidor? | Basta o front re-buscar? |
|---|---|---|
| **Local** — `cerne feature …` via CLI no mesmo host, agente em background, ou ação no próprio UI | Sim — escrevem no mesmo clone de cache (`CommitAndSnapshot`/`CommitAndPush`) | ✅ |
| **Remota** — colega dá push no backend (GitLab) | **Não** — o servidor só faz `git pull` no *start* ([`runUI` → `resolveProject`](../../../../internal/cli/context.go)) | ❌ exige pull periódico no servidor |

Logo: atualizar mudanças **locais** depende só de fechar a malha no front; cobrir mudanças **remotas** exige o servidor passar a fazer `pull` em intervalo enquanto roda — o que relaxa, de forma deliberada e opcional, o "offline-by-default" atual (hoje o pull só ocorre no start).

## Objetivo e critérios de sucesso

1. **Malha fechada para mudanças locais.** Com `cerne ui` rodando e a tela aberta, qualquer mutação que avance o backend local (CLI em outro terminal, agente, outra aba do UI) reflete na tela em **≤ ~2 s, sem F5**.
2. **Cobertura de mudanças remotas.** Um push de outro autor no backend reflete na tela sem F5, em **≤ `pull-interval` + latência de detecção**.
3. **Transporte: SSE.** Um endpoint `GET /api/stream` (`text/event-stream`) emite um evento `change` (com o novo SHA do backend) quando o backend avança; o front mantém um `EventSource` aberto e re-busca a view atual ao receber. Mantém keep-alive; encerra **sem vazar goroutine** quando o cliente desconecta.
4. **Pull periódico opcional.** `cerne ui --pull-interval <dur>` faz o servidor dar `gitback.Pull` em intervalo; `--pull-interval 0` **desliga** (restaura o comportamento atual, offline). O pull não notifica o hub diretamente: ao trazer commits, ele move o HEAD, e o **mesmo** observador percebe — um único caminho de notificação.
5. **Sem flicker, sem regressão de contrato.** O re-fetch automático **não** zera a tela para "Carregando…" (mantém os dados antigos até chegar o novo estado). Nenhum DTO da API muda; nenhum endpoint existente muda de forma.
6. **Degradação graciosa.** Sem SSE (navegador antigo, conexão caiu antes de reconectar) a tela continua funcional e o F5 segue valendo. O `EventSource` reconecta sozinho; ao reconectar, o servidor manda o SHA atual para ressincronizar (cobre mudanças perdidas no intervalo desconectado).
7. **Suíte verde.** `go vet ./...` + `go test ./...` + `go build ./cmd/cerne` verdes; UI rebuildada com `npm ci && npm run build` e `ui/dist` commitado.

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Qual é o "sinal de mudança"** | O **SHA do HEAD do clone local** (`gitback.HeadSHA(repoDir)`), não o log de eventos | Generaliza o "watch do event log": **toda** mutação passa por commit (`CommitAndSnapshot`/`CommitAndPush`) — lifecycle, edição de doc, artifact, referência — e todo pull que traz algo move o HEAD. Um único sinal cobre tudo; o event log perderia edições de doc puras. |
| **Como detectar** | **Poll do `HeadSHA` no servidor** a cada ~1 s, comparando com o último visto | Sem dependência nova (evita `fsnotify`); custo desprezível (uma leitura de ref do git por segundo); robusto e idêntico no Windows (host de dev) e Linux (CI). |
| **Transporte** | **SSE** (não WebSocket) | A notificação é unidirecional servidor→cliente; SSE é HTTP simples (mesmo mux, mesma porta), reconecta nativamente e não adiciona dependência. WebSocket seria bidirecional sem necessidade. |
| **O que o evento carrega** | Só `change` + o SHA atual; o cliente **re-busca** o endpoint da view | Mantém o servidor como fonte única de verdade e o front trivial; evita um segundo "formato de mudança" paralelo aos DTOs. Diff granular por entidade é over-engineering aqui. |
| **Pull remoto** | Goroutine **separada** do hub; só faz `Pull`. A notificação sai do observador de HEAD | Um caminho único de notificação (commit local e pull remoto convergem em "o HEAD mexeu"). Erro de pull (offline) é logado e ignorado — nunca derruba o servidor. |
| **Default do pull** | Ligado, **15 s** (`--pull-interval 0` desliga) | O dono pediu cobertura remota; em uso interativo (dev na frente da tela) um pull a cada 15 s é barato e responsivo. O opt-out preserva quem quer offline estrito. *(valor a confirmar na revisão.)* |
| **Ciclo de vida das goroutines** | Observador + pull amarrados a um `context.Context`; cancelados no shutdown e no fim de cada teste | Evita goroutine vazada (importa para a suíte e para `httptest`). Implica `context` chegar ao `server` (ver Desenho). |
| **Onde fica o estado no front** | Um **contexto global** com um contador `version` + hook `useLiveVersion()`; cada tela inclui `version` nas deps do seu `useEffect` de fetch | Centraliza o `EventSource` (uma conexão para todo o app) em vez de espalhar timers/sockets pelas ~7 telas; cada tela ganha uma linha. |
| **Escopo de servidor** | `cerne ui` (single-project, [`server.New`](../../../../internal/server/server.go)) | É o que o usuário usa. O multiprojeto (`cerne serve`, [`multiproject.go`](../../../../internal/server/multiproject.go)) tem registro dinâmico de N projetos e fica como follow-up. |

---

## Desenho

### `internal/server` — hub SSE + observador de HEAD
- **`hub`** (novo, ex.: `internal/server/live.go`): broadcaster thread-safe. Registra/desregistra assinantes (cada um um `chan string`), `broadcast(sha)` envia a todos sem bloquear (drop/skip se o canal estiver cheio — o cliente re-busca de qualquer forma no próximo evento).
- **observador**: goroutine que faz poll de `gitback.HeadSHA(repoDir)` (~1 s), e quando o SHA difere do anterior chama `hub.broadcast(sha)`. Amarrada ao `ctx`.
- **`GET /api/stream`** (`handleStream`): seta `Content-Type: text/event-stream`, `Cache-Control: no-cache`; usa `http.Flusher`. Manda o SHA atual de imediato (ressincroniza no connect/reconnect), depois faz `select` entre (novo SHA → `event: change\ndata: <sha>\n\n`), (ticker de keep-alive → comentário `:\n\n`) e (`r.Context().Done()` → retorna, desregistrando no `defer`).
- **assinatura**: `server.New` passa a receber um `context.Context` e a opção de `pull-interval` (struct de opções ou parâmetro). Registra `/api/stream` e inicia observador + (se intervalo > 0) o loop de pull. *(forma exata da assinatura — variádica de opções vs. struct — decidida no `plan.md`; os call sites são `uicmd.go` + testes.)*

### `internal/cli/uicmd.go`
- Nova flag `--pull-interval` (default 15 s). Cria um `context.Context` cancelado no sinal de término (Ctrl+C) e o passa a `server.New`. Sem `--no-browser`-style surpresa: o comportamento default ganha o auto-refresh; `--pull-interval 0` volta ao offline.

### `internal/gitback`
- Reuso direto de `HeadSHA` e `Pull`; nenhum novo helper previsto. (Se o poll de `HeadSHA` mostrar custo, otimização futura: stat de `.git/HEAD`/ref — fora de escopo agora.)

### `ui/` — uma conexão, um contador de versão
- **`ui/src/live.tsx`** (novo): `LiveProvider` abre `new EventSource('/api/stream')`, incrementa `version` a cada `change`; expõe via contexto. `useLiveVersion(): number`. Reconnect é nativo do `EventSource`.
- **`ui/src/api.ts`**: helper fino `subscribeChanges(onChange)` encapsulando o `EventSource` (mantém o padrão "api.ts espelha o servidor").
- **`ui/src/App.tsx`**: envolve `<Routed>` em `<LiveProvider>`.
- **telas de estado** (Kanban, FeaturePage, Decisions lista+detalhe, References lista+detalhe, Lint, History, Graph): `const version = useLiveVersion()` e `version` nas deps do `useEffect` de fetch. **Não** voltar o estado a `null` no re-fetch (sem flicker). Telas **sob demanda** (Search, Ask) ficam de fora — disparam por input do usuário.

---

## Verificação (TDD)

- **`internal/server` (hub):** `TestHubBroadcastsToSubscribers` (N assinantes recebem o SHA; desinscrição não bloqueia o broadcast).
- **`internal/server` (stream):** `TestStreamEmitsChangeOnHeadChange` — via `httptest`, com um `repoDir` cujo HEAD avança (um commit no clone): o response body recebe um frame `event: change` com o novo SHA; `TestStreamSendsCurrentShaOnConnect` (ressincronização imediata); `TestStreamUnsubscribesOnClientCancel` (cancelar o `ctx` do request encerra o handler — sem goroutine vazada).
- **`internal/server` (pull loop):** `TestPullLoopAdvancesHeadFromRemote` — backend "remoto" recebe um commit; com `pull-interval` curto, o observador detecta e o hub emite. `pull-interval = 0` → nenhum pull.
- **`internal/cli`:** `TestUIPullIntervalFlag` (parse/forward da flag; default e `0`).
- **`ui/`:** typecheck/build (`npm run build`); o `dist` rebuildado entra no commit. (Sem framework de teste de componente no projeto hoje; verificação de UI é build + e2e manual descrito no `result.md`.)
- `go vet ./...` + suíte completa verdes; `go build ./cmd/cerne` ok; `cerne ui` sobe e a malha fecha (passo manual no `result.md`).

## Fora de escopo

- **Multiprojeto (`cerne serve`).** Auto-refresh por projeto, com registro dinâmico de N backends, é um follow-up — este caso entrega o `cerne ui` single-project.
- **Atualização diferencial/granular** (mandar *o que* mudou e dar patch no cliente). Por ora: "o backend mexeu → a view re-busca". Suficiente e simples.
- **`fsnotify`** como mecanismo de detecção — recusado a favor do poll de `HeadSHA` (sem dependência nova).
- **Telas sob demanda** (Search, Ask) — não há "estado vigente" a observar; disparam por input.
- **Indicador visual de "atualizado"** (toast/realce) — nice-to-have, não requisito; pode virar follow-up se a atualização silenciosa confundir.
- **Lint de wiki-links** (`[[note]]`) — gap conhecido, não relacionado.
