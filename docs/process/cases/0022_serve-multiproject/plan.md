# Plano — Case 0022 (`cerne serve` multi-projeto)

**Goal:** um processo cerne serve N projetos via `/api/projects/{pid}/*` (reusando os handlers atuais), com registro de backend por projeto e auth S2S — fundação para o Studio consumir.

## Tarefa 1 — mux multi-projeto + resolver + registro + auth (TDD)
`internal/server/multiproject.go` + `multiproject_test.go`:
- `MultiServer{root, secret}` com `Handler()` registrando `/api/projects/{pid}/...` que extrai `pid`, resolve `(repoDir, projDir, projectID)` sob `root/<pid>/{repo,proj}` e chama os handlers atuais (`handleState`, `handleFeatureDetail`, `handleCreateFeature`, `handleDecisionDetail`, `handleReferenceDetail`, `handleCreateReference`, `handleSearch`, `handleAsk`, `handleWikiIndex`, `handleWikiPage`, `handleLLMConfigGet/Put`, artifacts).
- `PUT /api/projects/{pid}` `{backend}` → garante o clone (`gitback.Clone`/recreate em `root/<pid>/repo`) + `project.SaveLock(root/<pid>/proj)`. Idempotente.
- `requireSecret` middleware (Bearer == `secret`; 401 caso contrário); pid não registrado → 404.
- Testes httptest: registrar 2 backends bare locais; estados isolados; `POST features` em `a` não vaza em `b`; 401 sem/secret errado; 404 pid desconhecido.
→ commit `feat(server): multi-project serve mux + per-project register + S2S auth`.

## Tarefa 2 — comando `cerne serve` (TDD leve)
`internal/cli/servecmd.go` + `root.go`: `cerne serve --root --port`; lê `CERNE_SERVE_SECRET` (fail-fast se vazio); `net.Listen` + `http.Serve(MultiServer.Handler())`; sem browser. Teste: fail-fast sem secret; wiring do comando.
→ commit `feat(cli): cerne serve — multi-project HTTP service`.

Verificar: `go vet` + suíte verdes.

## Fechamento (cerne)
- `result.md`; status spec → Done; índice `docs/process/CLAUDE.md`; `scripts/build-release.sh`/Dockerfile do serve = no case do hub (deploy).

## Próximo (lado hub, case em apps/studio — após a fundação)
- Container `cerne-service` (Dockerfile do serve) + compose + Traefik + env (`CERNE_INTERNAL_URL`, `CERNE_SERVE_SECRET`/shared-secret).
- BFF `app/api/projects/[id]/docs/**` (proxy S2S + gate de visibilidade) + provisionar `<projeto>-docs` no Gitea + registrar no cerne-service.
- Página "Documentação" nativa (Arc): Kanban + criar feature + Wiki + Perguntar.
