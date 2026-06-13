# Plano — Case 0009 (UI: nav shell + páginas de leitura)

**Goal:** Expor decisions, references, busca full-text, lint e o event log na UI, atrás de um shell de navegação, reusando o núcleo determinístico existente. Backend primeiro (testável em Go, TDD), depois frontend (sem framework de teste JS no projeto — verificado por build + checagem manual no hub, como o kanban/feature atuais).

## Tarefas (TDD, uma por commit)

1. **Servidor recebe `ProjectID` + detalhe de decision/reference.** Estender `server.New(repoDir, projDir, projectID)`; `uicmd` passa `ctx.cfg.ProjectID`. Adicionar `GET /api/decisions/{id}` → `{decision, doc}` e `GET /api/references/{id}` → `{reference, doc}`, espelhando `handleFeatureDetail` (entidade via `store.LoadDecision`/`LoadReference`; doc de `content/decisions/<id>.md` / `content/references/<id>.md`; 404 em `ErrNotFound`). Testes (`newBackend`): existente devolve entidade+doc; ausente → 404 pt-BR. → commit `feat(server): decision and reference detail endpoints`.

2. **`GET /api/search`.** Embrulha `index.Search`; resolve `project.IndexPath(projectID)`; reconstrói o índice se faltar (igual a `runSearch`). `q` (obrigatório, 400 se vazio), `limit` (default 10). Devolve `{results}`. Testes: semear entidades incl. uma feature supersedida; a busca acha o hit (com sucessor); `q` vazio → 400; índice construído na 1ª chamada (sob `CERNE_CACHE_DIR` temпорário). → commit `feat(server): full-text search endpoint`.

3. **`GET /api/lint`.** Embrulha `lint.Run(repoDir)` → `{findings}` na ordem `lint.RuleOrder`. Testes: backend com staleness conhecida (ex.: `missing-gate-doc` — feature implemented sem result real) devolve o achado; backend limpo → `findings` vazio. → commit `feat(server): lint endpoint`.

4. **`GET /api/events`.** Embrulha `store.ReadEvents(repoDir)`, invertido (mais novo primeiro) → `{events}`. Testes: append de 2+ eventos, endpoint devolve do mais novo pro mais antigo. → commit `feat(server): event log (history) endpoint`.

5. **Shell de navegação + roteamento (frontend).** `App.tsx`: tabela de rotas + `<Nav>` persistente; rota atual destacada; desconhecida → kanban. `api.ts`: tipos + fetchers (`fetchDecision`, `fetchReference`, `search`, `fetchLint`, `fetchEvents`). → commit `feat(ui): navigation shell and route table`.

6. **Páginas Decisions + References.** Lista agrupada por estado (do `/api/state`) + detalhe via os endpoints novos (`ReactMarkdown`). → commit `feat(ui): decisions and references pages`.

7. **Páginas Busca + Lint + Histórico.** Busca (input + hits); Lint (achados por regra + estado vazio); Histórico (timeline). → commit `feat(ui): search, lint and history pages`.

8. **Rebuild `ui/dist` + verde total.** `cd ui && npm run build`, commitar `dist`. `go vet ./... && go test ./... -count=1`. → commit `chore(ui): rebuild dist with the new pages`.

## Verificação final

- `go vet ./...` + `go test ./... -count=1` verdes (suíte atual + novos testes de handler).
- Dogfood no clone do hub: `cerne ui` → as cinco páginas funcionam; busca acha uma entidade supersedida com o sucessor; lint mostra achados reais; histórico mostra o stream de eventos. Números/observações no result.md.
