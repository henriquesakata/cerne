# Resultado — Case 0017 (Wiki na web: perguntar + navegar)

**Status:** Done (2026-06-13). Suíte: **433 testes** verdes, `go vet` limpo, build da UI ok (sem dep nova; mermaid segue lazy). Smoke real na assinatura **através do servidor** ✅.

## O que foi entregue

A divisão **CLI = agente / web = humano** ganhou a camada de IA no navegador: o humano **pergunta** à wiki em linguagem natural (resposta via provider) e **lê** a síntese compilada por módulo.

### Frente A — backend (Go)

- **`POST /api/ask`** (`handleAsk` em `internal/server/server.go`): body `{query, history}`; query vazia → 400; recupera contexto marcado por estado com `ask.Retrieve` (mesmo índice FTS offline da Busca, rebuild na 1ª chamada); projeta `sources` (`{source, kind, state}`). A config é lida **a cada request** via `project.LoadConfig(projDir)` → em delegação (ou sem `.cernerc`) devolve `{answered:false, needs_provider:true, sources}` — **nunca inventa resposta**; com `byo-key`/`claude-cli` chama `prov.Complete(BuildAskPrompt)` → `{answered:true, answer, sources}`. Commit `14ac19b`.
- **Navegar a wiki** (`handleWikiIndex` + `handleWikiPage`): `GET /api/wiki` lista as páginas existentes em `content/wiki/` (transversais + `modules/*`) com nome canônico; `GET /api/wiki/{rel...}` serve o corpo sem o frontmatter de proveniência, com `safeWikiRel` barrando traversal (`..`, absoluto, oculto, backslash). Exporta `ask.ReadWikiPage` para servidor e `ask` lerem as mesmas páginas. Commit `0aa4305`.

### Frente B — UI (React)

- **`api.ts`**: `ask(query, history)`, `fetchWikiIndex()`, `fetchWikiPage(rel)` + tipos (`AskSource`, `AskAnswer`, `WikiPageRef`, `WikiPage`). `fetchWikiPage` codifica segmentos preservando a barra (rota `{rel...}`).
- **`Ask.tsx`** (Perguntar): textarea + toggle de histórico; resposta em `Markdown`; fontes lincadas (`sourceHref`: `wiki:<rel>`→`#/wiki/<rel>`, `D-`/`R-`/feature); em `needs_provider`, aviso "configure `[llm] mode` no `.cernerc` (relido a cada pergunta)" + as fontes.
- **`Wiki.tsx`** (Wiki): índice (Visão geral: project/decisions/references; Módulos) + leitor (`#/wiki/<rel>` → `Markdown`).
- **`App.tsx`/`Nav.tsx`**: links + rotas `#/ask`, `#/wiki`, `#/wiki/<rel>`; CSS em `app.css` (`.ask-history`, `.ask-notice`, …). Sem dep nova (reusa o componente `Markdown`). Commit `1a61b99`.

## Decisão revista (vs spec original)

A spec abriu com "threading: `server.New(..., llmCfg)`". Trocado por **carga em tempo de request** (`project.LoadConfig(projDir)` no `handleAsk`): evitou churn em ~40 chamadas de `New(...)` nos testes (menor risco de conflito com o agente concorrente no `server_test.go`) **e** entrega hot-reload — o usuário adiciona `[llm] mode="claude-cli"` no `.cernerc` e o Perguntar passa a responder **sem reiniciar** `cerne ui`. spec/plan já refletem isso.

## Verificação

| Item | Como | Resultado |
|---|---|---|
| `POST /api/ask` byo-key | server test (provider fake httptest + `.cernerc`) | `answered:true`, resposta do provider, `sources` projetadas |
| `POST /api/ask` delegação | server test (sem `.cernerc`) | `answered:false`, `needs_provider:true`, `sources` presentes, **sem** `answer` |
| query vazia | server test | 400 pt-BR |
| `GET /api/wiki` | server test | só páginas em disco; `[]` quando vazio |
| `GET /api/wiki/{rel}` | server test | corpo sem frontmatter; nested `modules/core`; 404 inexistente |
| traversal | unit `safeWikiRel` (tabela) | rejeita `..`/abs/oculto/`..%2F` |
| **smoke real (assinatura)** | gated, in-process httptest, `mode="claude-cli"` | **`POST /api/ask` → `claude -p` → resposta real não-vazia** (efêmero, não commitado) |
| UI | `npm run build` | `tsc` limpo; sem dep nova; mermaid/excalidraw lazy; entry +~5 kB |
| Go embute dist | `go test ./...` | 433 testes |

## Resposta ao pedido do usuário

"CLI = interface do agente; web = interface dos humanos; dá pra ver as perguntas da Wiki pela web?" → **Sim, agora dá.** O CLI (`ask`/`compile` em delegação) continua sendo o canal do agente (devolve prompt para o agente hospedeiro responder). Na web, onde não há agente hospedeiro, o servidor **chama o provider** (assinatura via `claude-cli`, ou `byo-key`) e devolve a resposta — e, quando não há provider, mostra as fontes sem inventar. Além de perguntar, o humano **lê** a síntese compilada (página Wiki).

## Inventário

- **Go:** `internal/server/server.go` (rotas + `handleAsk`/`handleWikiIndex`/`handleWikiPage`/`safeWikiRel` + DTOs + import `sort`/`ask`/`llm`), `internal/ask/ask.go` (`ReadWikiPage` exportado).
- **Testes:** `internal/server/ask_test.go`, `internal/server/wiki_test.go` (+8; 425→433).
- **UI:** `ui/src/api.ts`, `ui/src/Ask.tsx` (novo), `ui/src/Wiki.tsx` (novo), `ui/src/App.tsx`, `ui/src/Nav.tsx`, `ui/src/app.css`, `ui/dist` (rebuild).
- **Commits:** `f968f69` (abertura), `14ac19b`, `0aa4305`, `1a61b99` (+ este result).

## Fora de escopo / follow-ups

- **Streaming** da resposta (SSE) — hoje é síncrona; `claude-cli` pode levar segundos.
- **Editar/compilar** a wiki pela web — leitura apenas; `compile` (CLI/agente) é o caminho de autoria.
- Histórico de perguntas / permalink de uma pergunta.
- Verificação visual no browser (render) — coberta por build + revisão; não automatizada aqui.
