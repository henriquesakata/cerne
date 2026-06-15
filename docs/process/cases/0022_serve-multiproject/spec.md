# Case 0022 — `cerne serve`: modo multi-projeto (fundação da integração com hosts)

tags: #spec #cerne #serve #multiproject #service #integration

**Status:** Done (2026-06-14) — ver [result.md](result.md)
**Tipo:** Novo comando `cerne serve` + servidor HTTP multi-projeto (reusa os handlers atuais, parametrizados por `projectId`) + auth S2S por shared-secret. **Fundação** para hosts (ex.: Studio do Rockt Hub) consumirem o cerne de N projetos via um único serviço.
**Origem:** Pedido do usuário (no hub): integrar o cerne no Studio para gerir a documentação de cada aplicação. O Studio é web multiusuário com N projetos; o `cerne ui` atual é local, single-project. Esta é a fatia **F1, lado cerne**: o cerne passa a servir **vários projetos** num processo só, atrás de auth de serviço. (O lado hub — BFF + página Arc + container/compose — é case separado em `apps/studio`.)

---

## Problema

O `cerne ui` (`server.New(repoDir, projDir, projectID)`) serve **um** projeto: repoDir/projDir fixos no boot. Um host como o Studio precisa servir os docs de **muitos** projetos (um por aplicação), com **autenticação de serviço** (S2S), num único processo deployável. Hoje isso exigiria um processo por projeto (inviável). Os **handlers** (`handleState`, `handleFeatureDetail`, `handleDecisionDetail`, `handleReferenceDetail`, `handleSearch`, `handleAsk`, `handleWikiIndex`, `handleWikiPage`, `handleCreateFeature`, `handleCreateReference`, `handleLLMConfigGet/Put`, artifacts) já recebem `repoDir/projDir/id` por parâmetro — faltam o **roteamento multi-projeto**, a **resolução `projectId → (repoDir, projDir)`** e a **auth de serviço**.

---

## Objetivo e critérios de sucesso

1. **`cerne serve`** (novo comando): sobe um HTTP server multi-projeto. Flags: `--root <dir>` (raiz dos caches por projeto), `--port` (default ex. 7700), `--no-browser` implícito (serviço, sem browser). Auth via env `CERNE_SERVE_SECRET` (shared-secret S2S); ausente → recusa subir (fail-fast).
2. **Roteamento namespacado:** `/api/projects/{pid}/state`, `/features/{fid}`, `/features` (POST), `/decisions/{id}`, `/references/{id}`, `/references` (POST), `/search`, `/ask` (POST), `/wiki`, `/wiki/{rel...}`, `/config/llm` (GET/PUT), artifacts (GET/PUT) — **reusando os handlers atuais**, com `(repoDir, projDir)` resolvidos por request a partir do `{pid}`.
3. **Resolução de projeto:** dado `pid`, o serviço resolve `(repoDir, projDir)` sob `--root/<pid>/` (cache clone + docs.lock) a partir do **backend git daquele projeto**. O backend é **registrado pelo host** (cerne é agnóstico ao git-host): endpoint `PUT /api/projects/{pid}` `{backend}` registra/garante (clona se ausente; idempotente). Provisionar o repo Gitea em si é responsabilidade do host (Studio).
4. **Auth S2S:** middleware exige `Authorization: Bearer <CERNE_SERVE_SECRET>` (ou header equivalente) em todas as rotas `/api/*`; 401 sem/!=. A **autorização por projeto/usuário é do host** (o BFF do Studio gateia antes de proxiar) — o serviço confia no shared-secret.
5. **Sem SPA/UI no serve:** `cerne serve` expõe só a API (o host renderiza). Sem browser, sem embed.
6. **`cerne ui` (single-project) intacto:** o modo local continua igual; `serve` é adicional.
7. Suíte + `go vet` verdes; testes httptest do roteamento multi-projeto + auth + resolução.

---

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Reuso vs reescrever** | Mux multi-projeto que **resolve `pid`** e chama os **handlers existentes** | Os handlers já recebem `repoDir/projDir/id`; zero duplicação de lógica. |
| **Quem é dono do git-host** | cerne é **agnóstico**: recebe o `backend` (git URL) por registro (`PUT /api/projects/{pid}`) | Desacopla o cerne do Gitea; o Studio (que conhece o Gitea) cria o `<projeto>-docs` e registra a URL. |
| **Auth** | shared-secret S2S (env), fail-fast se ausente | Padrão dos serviços internos do hub; autorização fina fica no BFF do host. |
| **Layout dos caches** | `--root/<pid>/repo` (clone) + `--root/<pid>/proj` (docs.lock) | Espelha o layout single-project; isola por projeto. |
| **Provisionar repo** | Fora do cerne (host cria o repo Gitea); cerne só **clona/garante** | Separação de responsabilidades; cerne não fala Gitea API. |
| **SPA** | `serve` é API-only | O host renderiza nativo (Arc no Studio); embed quebraria o design system. |

---

## Desenho

### `internal/server`
- `NewMultiProject(root string, secret string, resolve func(pid string) (repoDir, projDir, projectID string, err error)) http.Handler` (ou um tipo `MultiServer`): registra as rotas `/api/projects/{pid}/...`; cada handler extrai `pid := r.PathValue("pid")`, resolve `(repoDir, projDir, projectID)` e chama o handler atual (`handleState(w, repoDir)`, etc.). 404/erro de resolução → JSON.
- Middleware `requireSecret(secret)` envolvendo o mux `/api/*`.
- `PUT /api/projects/{pid}` `{backend}` → registra/garante o projeto (clona o backend em `root/<pid>/repo` se ausente; grava docs.lock em `root/<pid>/proj`); reusa `gitback.Clone`/recreateCache + `project.SaveLock`.

### `internal/cli`
- `newServeCmd()`: `cerne serve --root --port`; lê `CERNE_SERVE_SECRET` (fail-fast); `net.Listen` + `http.Serve(server.NewMultiProject(...))`. Sem `openBrowser`.
- Resolver: dado `pid`, monta paths sob `--root`; se o cache falta mas o projeto foi registrado, recria do backend (reusa a lógica do case 0008).

### Provisionamento (mínimo no cerne)
- `PUT /api/projects/{pid}` garante o clone do `backend` informado. Se o backend ainda não existe como repo git remoto, é erro (host deve criar antes). `cerne init`-equivalente do layout só quando o host pedir um backend vazio (decisão de F2; F1 assume backend já inicializado pelo host ou pelo próprio ensure clonando um repo vazio + InitLayout + push).

---

## Verificação

- **httptest (multi-projeto):** registrar 2 projetos (`PUT /api/projects/{a|b}` com backends locais bare); `GET /api/projects/a/state` vs `/b/state` retornam estados distintos; `POST /api/projects/a/features` cria em `a` e não em `b`.
- **auth:** sem `Authorization` → 401; com secret errado → 401; com secret certo → 200.
- **resolução:** `pid` não registrado → 404 pt-BR; cache ausente de projeto registrado → recriado do backend.
- **fail-fast:** `cerne serve` sem `CERNE_SERVE_SECRET` → erro claro, não sobe.
- `go vet` + suíte verdes. (Smoke real do serviço + integração Studio fica no case do hub.)

---

## Fora de escopo (F2+ / lado hub)
- Página "Documentação" no Studio (Arc), BFF proxy, container/compose/Traefik/env do `cerne-service` → **case em `apps/studio`**.
- Provisionar o repo `<projeto>-docs` no Gitea (host).
- Provisionamento eager no create-app; edição de spec/plan/result via API; diagramas; multiusuário fino dentro do cerne (fica no BFF).
- Migrar o `cerne ui` single-project para cima do multi (mantidos lado a lado por ora).
