# Case 0017 — Wiki na web: perguntar (Ask) e navegar a síntese (humanos)

tags: #spec #cerne #ui #ask #wiki #llm

**Status:** Done (2026-06-13) — ver [result.md](result.md)
**Tipo:** Endpoints de servidor (`/api/ask`, `/api/wiki`) + páginas web (Perguntar, Wiki) + threading da config de LLM no servidor.
**Origem:** Pedido do usuário: _"gostaria que o CLI fosse a interface para o agente de IA e as páginas web a interface para humanos. As perguntas pra Wiki, seriam possível ver pela web?"_ — formalizar a divisão **CLI = agente / web = humano** e trazer a camada de IA (perguntar + ler a wiki) para o humano no navegador.

---

## Problema

A web é a interface dos humanos, mas hoje o humano **não consegue**:

1. **Perguntar à wiki em linguagem natural.** A UI tem *Busca* (keyword/FTS, `GET /api/search`), mas não *Ask* (Q&A). O `cerne ask` ([askcmd.go](../../../internal/cli/askcmd.go)) opera em **modo delegação** por default: ele monta o prompt (`ask.BuildAskPrompt`) e o **imprime** para o agente hospedeiro responder — devolve um _prompt_, não uma _resposta_. Não há `POST /api/ask`.
2. **Ler a síntese da wiki.** As páginas compiladas (`cerne compile`) vivem em `content/wiki/` (`project.md`, `decisions.md`, `references.md`, `modules/<x>.md`) e **não são navegáveis** na UI — o humano só vê entidades soltas, nunca a narrativa por módulo.

A divisão que o usuário descreveu **já é metade verdade**: no CLI, em delegação, **quem responde é o agente** (a própria LLM que roda o CLI). A consequência de design é que **um humano no navegador não tem agente hospedeiro** — para responder, o servidor precisa **chamar o provider** ele mesmo (`claude-cli`/assinatura do [case 0014](../0014_claude-cli-provider/spec.md), ou `byo-key`). Hoje o servidor (`server.New(repoDir, projDir, projectID)`) **nem recebe a config de LLM**.

---

## Objetivo e critérios de sucesso

1. **`POST /api/ask`** `{query, history}`: recupera o contexto marcado por estado (`ask.Retrieve`) e:
   - com provider que responde (`byo-key`/`claude-cli`): chama `prov.Complete` e devolve `{answered:true, answer, sources[]}`.
   - em **modo delegação** (sem ninguém para responder no servidor): devolve `{answered:false, needs_provider:true, sources[]}` — **nunca fabrica resposta**; as fontes recuperadas já são úteis (busca semântica enriquecida).
2. **Página "Perguntar"** na UI: textarea + toggle de histórico; resposta renderizada (componente `Markdown`/GFM); lista de fontes com link (entidade → `#/f|d|r/<id>`; página de wiki → `#/wiki/<rel>`). Em delegação, mostra as fontes + aviso de "configure um provider para respostas".
3. **`GET /api/wiki`** lista as páginas compiladas existentes; **`GET /api/wiki/{rel...}`** devolve o corpo de uma página (frontmatter de proveniência removido).
4. **Página "Wiki"** na UI: índice (project/decisions/references/módulos) + leitor (Markdown). Rotas `#/wiki` e `#/wiki/<rel>`.
5. A divisão fica explícita e graciosa: delegação = agente (CLI); web = humano via provider. Sem fabricar respostas, sem quebrar quando não há provider nem wiki compilada.
6. Suíte + `go vet` verdes; build da UI ok (sem deps novas — reusa `Markdown`).

---

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **LLM no servidor** | `handleAsk` carrega a config em **tempo de request** via `project.LoadConfig(projDir)` (sem mudar a assinatura de `server.New`) | Evita churn em ~40 chamadas de `New(...)` nos testes (menor risco de conflito com o agente paralelo) **e** dá hot-reload: o usuário adiciona `[llm] mode="claude-cli"` no `.cernerc` e o Ask responde sem reiniciar `cerne ui`. `LoadConfig` falho → trata como delegação. |
| **Ask em delegação na web** | Responder 200 com `answered:false` + `needs_provider:true` + `sources` (não 409/erro) | A web degrada com graça e **nunca inventa**; as fontes sozinhas já valem (como uma busca melhor). Mostrar as fontes > falhar. |
| **Reuso** | `ask.Retrieve` + `ask.BuildAskPrompt` + `llm.NewProvider` já existem; o endpoint é só fiação | Mínimo de código novo; mesma recuperação do CLI e da Busca (mesmo índice FTS). |
| **Fontes no DTO** | Projetar `[]ask.Snippet` → `{source, kind, state}` (sem o texto cru; link derivado de `source`) | Payload enxuto; a UI lincka. `wiki:<rel>` → `#/wiki/<rel>`; senão entidade. |
| **Navegar wiki: fonte da verdade** | `compile.Pages(repoDir)` dá a lista canônica (Name/Rel); o índice mostra só as que **existem em disco** (`content/wiki/<rel>.md`) | Distingue compilada de pendente; nomes canônicos sem reparsear nomes de arquivo. |
| **Corpo da wiki** | Servir com o frontmatter de proveniência removido (extrair helper de `stripFrontmatter`, hoje privado em `ask`) | O humano lê a síntese, não o frontmatter `cerne_wiki`. |
| **Síncrono, sem streaming** | A resposta do provider é síncrona (igual compile/ask byo-key); `claude-cli` pode levar segundos | v1 simples; streaming (SSE) fica fora de escopo. |

---

## Desenho

### Backend (Go)

- **`internal/server/server.go`**: novas rotas (sem mudar `New`):
  - `POST /api/ask` → `handleAsk`: lê `{query, history}`; valida query não-vazia (400); `dbPath := project.IndexPath(projectID)`; rebuild offline se faltar (igual `handleSearch`); `snips := ask.Retrieve(repoDir, dbPath, query, history, 10)`; projeta `sources`. Carrega `cfg, _, err := project.LoadConfig(projDir)`; se `err != nil` ou `llm.ResolveMode(cfg.LLM.Mode) == ModeDelegation` → `{answered:false, needs_provider:true, sources}`. Senão `prov := llm.NewProvider(cfg.LLM)`; `answer := prov.Complete(ctx, ask.BuildAskPrompt(query, snips))` → `{answered:true, answer, sources}`.
  - `GET /api/wiki` → `handleWikiIndex`: `compile.Pages(repoDir)`, filtra as que têm `content/wiki/<rel>.md` em disco; devolve `{pages:[{name, rel}]}`.
  - `GET /api/wiki/{rel...}` → `handleWikiPage`: lê `content/wiki/<rel>.md` (path saneado, sem `..`); 404 se não existe; devolve `{rel, body}` com frontmatter removido.
- **`internal/wiki`** (ou `ask`): exportar `StripFrontmatter`/leitura de página reutilizável (hoje `ask.readWiki`/`stripFrontmatter` são privados). Preferir um helper único para servidor e `ask` não divergirem.

### UI

- **`api.ts`**: `ask(query, history) → {answered, answer?, needs_provider?, sources: AskSource[]}` (`AskSource = {source, kind, state}`); `fetchWikiIndex() → WikiPage[]`; `fetchWikiPage(rel) → {rel, body}`.
- **`Ask.tsx`** (Perguntar): form (textarea + toggle histórico + enviar); estados loading/erro; resposta em `Markdown`; lista de fontes lincadas; quando `needs_provider`, aviso "Configure `[llm] mode` (claude-cli/byo-key) no `.cernerc` para respostas" + as fontes.
- **`Wiki.tsx`** (Wiki): índice das páginas (project, decisions, references, módulos) e leitor (`#/wiki/<rel>` → `fetchWikiPage` → `Markdown`).
- **`App.tsx`/`Nav.tsx`**: links/rotas `#/ask` e `#/wiki` (+ `#/wiki/<rel>` para o leitor).
- `sourceHref(source)`: `wiki:<rel>` → `#/wiki/<rel>`; `D-*` → `#/d/`; `R-*` → `#/r/`; senão `#/f/`.

---

## Verificação

- **Server (httptest):**
  - `POST /api/ask` byo-key (provider fake via httptest) → `answered:true`, `answer` do provider, `sources` projetadas.
  - `POST /api/ask` em delegação → `answered:false`, `needs_provider:true`, `sources` presentes, **sem** `answer`.
  - query vazia → 400.
  - `GET /api/wiki` → só páginas existentes em disco; `GET /api/wiki/{rel}` → corpo sem frontmatter; página inexistente → 404; `..` no path → 400/404.
- **Smoke real (assinatura):** `mode="claude-cli"`, subir o servidor, `POST /api/ask` responde de verdade (sem API key).
- **UI:** `npm run build` verde (sem dep nova); `go test ./...` (servidor embute o dist) verde; revisão das páginas Perguntar/Wiki (render no browser não automatizável aqui).

---

## Fora de escopo / staging

- **Streaming** da resposta (SSE) — v1 é síncrona.
- **Editar** páginas de wiki pela web — leitura apenas; `compile` é o caminho de autoria.
- **Disparar `compile`** pela web — a wiki é compilada via CLI/agente.
- **Auth** — ferramenta local single-user.
- Implementação em 2 frentes: **backend** (threading + `/api/ask` + `/api/wiki`) primeiro, **UI** (páginas Perguntar/Wiki + nav) depois, re-sincronizando com o agente concorrente.
