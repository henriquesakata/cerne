# Resultado — Case 0022 (`cerne serve` multi-projeto)

**Status:** Done (2026-06-14) — lado cerne da fatia F1. Suíte: **462 testes** verdes, `go vet` limpo.

## O que foi entregue

Um processo cerne passa a servir **N projetos** sob `/api/projects/{pid}/*`, reusando integralmente os handlers single-project. É a fundação para o Studio (e qualquer host) gerir os docs de cada aplicação.

- **`internal/server/multiproject.go`** — `NewMultiProject(secret) http.Handler`:
  - Resolve `(repoDir, projDir, projectID)` por request a partir de `{pid}` sob `project.CacheDir(pid)` (`repo/` + `proj/`, índice irmão), via `withProject` → chama os **handlers existentes** (`handleState`, `handleFeatureDetail`, `handleCreateFeature`, `handleDecisionDetail`, `handleReferenceDetail`, `handleCreateReference`, `handleSearch`, `handleAsk`, `handleWikiIndex`, `handleWikiPage`, `handleLLMConfigGet/Put`, `handleEvents`, `handleLint`). Zero duplicação de lógica.
  - **`PUT /api/projects/{pid}`** `{backend}` → `ensureProjectClone` (clona o backend git em `repo/` se ausente + grava `docs.lock` em `proj/`); idempotente.
  - **Auth S2S:** `requireSecret` exige `Authorization: Bearer <secret>` em tudo (401 caso contrário). Projeto não registrado → 404. **A autorização por usuário/projeto é do host** (o cerne confia no shared-secret).
  - Commit `037c8f96`.
- **`internal/cli/servecmd.go`** — `cerne serve --root --port`: lê `CERNE_SERVE_SECRET` (fail-fast se vazio); aponta `CERNE_CACHE_DIR`=`--root` (clone/índice/lock de cada projeto sob ele); `http.Serve(NewMultiProject(secret))`. API-only (sem SPA). `cerne ui` single-project intacto. Commit `e3140892`.

## Decisões (confirmadas)
| Fork | Decisão |
|---|---|
| Reuso | mux resolve `pid` e chama os handlers atuais (recebem `repoDir/projDir/id`). |
| Git-host | cerne **agnóstico**: recebe o `backend` por registro; o host (Studio) cria o repo Gitea. |
| Auth | shared-secret S2S (env, fail-fast); autorização fina no host. |
| Layout | `CacheDir(pid)/{repo,proj}` + `index.db` irmão; `--root`→`CERNE_CACHE_DIR`. |
| SPA | serve é API-only; o host renderiza (Arc no Studio). |

## Verificação
| Item | Como | Resultado |
|---|---|---|
| isolamento | httptest 2 projetos | estados distintos por `pid` |
| create sem vazamento | httptest | `POST features` em `a` não aparece em `b` |
| auth | httptest | sem/secret errado → 401 |
| projeto desconhecido | httptest | 404 |
| fail-fast | cli test | `serve` sem `CERNE_SERVE_SECRET` → erro citando a var |
| suíte+vet | `go test ./...` / `go vet` | 462 testes; limpo |

## Resposta ao pedido (lado cerne)
O cerne agora pode ser **deployado como serviço do hub** servindo todos os projetos do Studio num processo, atrás de auth de serviço, reusando toda a API. É o que o BFF do Studio vai proxiar.

## Inventário
- **Go:** `internal/server/multiproject.go` (+test), `internal/cli/servecmd.go` (+test), `internal/cli/root.go`.
- **Commits:** `deb3db08`, `037c8f96`, `e3140892` (+ este result).

## Próximo (lado hub — case em `apps/studio`)
- Dockerfile do `cerne serve` + container `cerne-service` no compose + Traefik + env (`CERNE_INTERNAL_URL`, `CERNE_SERVE_SECRET`).
- BFF `app/api/projects/[id]/docs/**` (proxy S2S + gate de visibilidade) + provisionar `<projeto>-docs` no Gitea + `PUT` de registro no cerne-service.
- Página "Documentação" nativa (Arc): Kanban + criar feature + Wiki + Perguntar.

## Fora de escopo (F2+)
- Transições/edição de docs/artifacts via multi-projeto (fácil de somar depois).
- Provisionar repo vazio + InitLayout pelo próprio cerne (hoje assume backend clonável).
