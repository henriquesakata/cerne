# Resultado — Case 0009 (UI: nav shell + páginas de leitura)

**Status:** Done — implementado e verificado no corpus real do hub (2026-06-13).
**Commits:** 6 de implementação (4 de servidor + 1 de frontend + 1 de rebuild do dist), sobre o `docs(cerne): open cases 0009/0010`.

---

## O que entrou

**Servidor (`internal/server`, TDD — 5 endpoints novos, +10 testes de handler):**
- `GET /api/decisions/{id}` e `GET /api/references/{id}` → entidade + markdown do doc de conteúdo, espelhando o detalhe de feature (404 pt-BR quando ausente).
- `GET /api/search?q=&limit=` → embrulha `index.Search` sobre todo o histórico; constrói o índice on-demand (offline, como `searchcmd`); projeção snake_case de `index.Result`; `q` vazio → 400.
- `GET /api/lint` → embrulha `lint.Run`, achados na ordem `lint.RuleOrder` (DTO snake_case).
- `GET /api/events` → embrulha `store.ReadEvents`, invertido (mais novo primeiro); `domain.Event` serializa direto.
- `server.New` ganhou `projectID` (uicmd passa `ctx.cfg.ProjectID`) para resolver `project.IndexPath`.

**Frontend (`ui/`):** shell de navegação persistente (`Nav.tsx`) + tabela de rotas em `App.tsx` (hash router, sem react-router); rotas de detalhe `#/d/<id>`, `#/r/<id>` espelhando `#/f/<id>`. Cinco páginas: Decisões, Referências (lista por estado + detalhe markdown), Busca (input + hits linkando ao detalhe), Lint (achados por regra + estado vazio), Histórico (timeline). `api.ts` ganhou os fetchers/tipos. `ui/dist` reconstruído (`npm ci` + `npm run build`).

---

## Verificação (dogfood no clone do hub, portas isoladas 7788/7789)

Suíte: `go vet ./...` limpo, `go test ./... -count=1` = **352 testes, 21 pacotes, verde**; `tsc --noEmit` limpo; bundle contém o código novo.

Endpoints + renderização contra o backend real do hub (dado antes invisível na UI):
- **Decisões: 45** (44 ativas, 1 substituída) — antes nenhuma aparecia na UI. Ex.: D-0001 "ADR-001: Micro-frontend Architecture".
- **Referências: 66** (vigentes) — idem, zero na UI antiga.
- **Busca** `identity` → hits sobre o histórico (F-0291, F-0286 implemented) com snippet `«identity»`.
- **Lint: 275 problemas** agrupados — 89 menção-a-substituída, 25 link-quebrado, 161 gate-doc-ausente (ex.: D-0001 menciona H-0001 "Bills" → use Finance).
- **Histórico: 421 eventos**, mais recente primeiro, timestamp localizado pt-BR (H-0002 `historical.recorded`).
- Nav destaca a seção corrente; kanban intacto (Candidatas 15 · Em dev 6 · Implementadas 285); **zero erros de console**.

Todos os 7 critérios de aceite da [spec](spec.md) atendidos.

---

## Débitos / notas

- **Sem testes unitários de JS** no projeto: o frontend foi verificado por `tsc` + inspeção do DOM ao vivo (o screenshot do preview MCP travou — limitação do mecanismo de captura no ambiente, não do código; o `eval` respondia na hora com o DOM correto).
- Índice de busca **não** auto-atualiza a cada mutação — `cerne pull` segue o ponto de sync (decisão da spec).
- Decisões/referências são **read-only** na UI; o ciclo delas continua na CLI.
- Numerado **0009** (a spec nasceu como 0008, mas `0008_cache-auto-recreate` ocupou o número em paralelo; reconciliado contra `origin/main` antes de implementar).

## Próximo

[Case 0010](../0010_feature-authoring-ui/spec.md) — autoria e gestão de features na UI (criar/editar spec/encerrar), que depende deste shell de navegação.
