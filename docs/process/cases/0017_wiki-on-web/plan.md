# Plano — Case 0017 (Wiki na web: Ask + navegar)

**Goal:** humanos perguntam à wiki em linguagem natural pela web (resposta via provider) e leem a síntese compilada por módulo. CLI/delegação = agente; web = humano via provider.

## Frente A — backend (dispatch 1, TDD)

Decisão (vs spec original): **sem mudar `server.New`**. O `handleAsk` carrega a config em tempo de request via `project.LoadConfig(projDir)` — evita o churn de ~40 chamadas de `New(...)` nos testes (menor risco com o agente paralelo) e dá hot-reload do provider.

1. **`POST /api/ask`** (`handleAsk` + teste httptest): body `{query, history}`; query vazia → 400; `IndexPath` + rebuild offline se faltar (igual `handleSearch`); `ask.Retrieve` → projeta `sources` (`{source, kind, state}`). `project.LoadConfig(projDir)`; erro ou delegação → `{answered:false, needs_provider:true, sources}` (sem `answer`). byo-key (config `.cernerc` com provider fake via httptest base_url) → `prov.Complete(BuildAskPrompt)` → `{answered:true, answer, sources}`. → commit `feat(server): POST /api/ask — answer over the wiki via provider`.

2. **Navegar a wiki** (`handleWikiIndex` + `handleWikiPage` + testes): exportar `ask.ReadWikiPage(repoDir, rel) (string, bool)` (refatorando o `readWiki` privado para reusá-lo). `GET /api/wiki` = `compile.Pages` filtradas pelas que existem em `content/wiki/<rel>.md` → `{pages:[{name, rel}]}`. `GET /api/wiki/{rel...}` = corpo sem frontmatter via `ask.ReadWikiPage`; 404 se não existe; rejeitar `..` (sanitizar path). → commit `feat(server): browse compiled wiki pages`.

Verificar: `go vet` + suíte verdes; **smoke real** `mode="claude-cli"` → subir servidor, `POST /api/ask` responde de verdade. Push (rebuild após rebase!).

## Frente B — UI (dispatch 2, após re-sync)

4. **`api.ts`**: `ask(query, history)`, `fetchWikiIndex()`, `fetchWikiPage(rel)` + tipos (`AskSource`, `AskAnswer`, `WikiPage`). **`Ask.tsx`** (Perguntar): textarea + toggle histórico + enviar; resposta em `Markdown`; fontes lincadas (`sourceHref`); aviso quando `needs_provider`. **`Wiki.tsx`** (Wiki): índice + leitor (`#/wiki/<rel>` → `Markdown`). **`App.tsx`/`Nav.tsx`**: rotas/links `#/ask` e `#/wiki` (+ `#/wiki/<rel>`). Build (`npm run build`, sem dep nova), commitar `ui/dist`. → commit `feat(ui): Perguntar (ask) + Wiki browse pages`.

Verificar: `go test ./...` (servidor embute o dist novo) verde; `cerne ui` serve as duas páginas.

## Fechamento

- `result.md` no case; status da spec → Done; linha 0017 no índice `docs/process/CLAUDE.md`.
- `scripts/update-cerne.ps1` para o binário do PATH ganhar os endpoints + páginas.
