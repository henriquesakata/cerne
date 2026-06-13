# Plano — Case 0012 (Gestão e autoria de features na UI)

**Goal:** Fechar o loop de autoria na UI (criar feature → escrever spec → transicionar → encerrar) e dar uma superfície de gestão por estado, reusando o core de `internal/app`. Backend TDD primeiro, depois frontend (verificado por `tsc` + dogfood, como o [case 0011](../0011_ui-read-pages/spec.md)). **Depende do shell de navegação do 0011.**

## Tarefas (TDD, uma por commit)

1. **`app.CreateFeature`** — extrair de `runFeatureNew` ([`featurecmd.go`](../../../../internal/cli/featurecmd.go)): `CreateFeature(repoDir, projDir, title, module string) (store.Feature, error)` faz `NextID` → `SaveFeature` (candidate) → stub de spec → evento `feature.created` → commit+push → atualiza lock. `runFeatureNew` passa a chamá-la (mantém a mensagem pt-BR da CLI). `validateModule` migra para `app` (compartilhado CLI+servidor). Testes em `internal/app`; CLI segue verde. → commit `refactor(app): extract CreateFeature shared by CLI and server`.

2. **`POST /api/features`** — body `{title, module}` → `app.CreateFeature` → 201 `{feature}`. Título vazio / módulo inválido → 422 pt-BR (mesmo contrato do transition). TDD em `server_test.go` (`newGitBackend` — confirma criação + push + lock + evento). → commit `feat(server): create feature endpoint`.

3. **`app.SaveFeatureDoc` + `PUT /api/features/{id}/docs/{doc}`** — grava `content/features/<id>/<doc>.md` (`doc ∈ spec|plan|result`) com o mesmo rabo commit+snapshot de `SaveArtifact` (no-op se byte-idêntico). Endpoint: 404 se feature não existe; 400 se `doc` fora do conjunto. TDD: gravar spec real faz o gate de `approve` passar. → commit `feat(server): edit feature lifecycle docs endpoint`.

4. **Frontend — criar + editar** — form "Nova feature" (título + módulo quando o projeto declara módulos) → `POST` → redireciona a `#/f/<novo-id>`. Abas spec/plan/result da página da feature viram editáveis (toggle editar/salvar, preview `ReactMarkdown`), sinalizando stub vs real. `api.ts`: `createFeature`, `saveFeatureDoc`. → commit `feat(ui): create feature and edit lifecycle docs`.

5. **Frontend — gestão por estado** — nova rota/página na nav (tabela de features, filtro por estado, ações de ciclo de vida inline reusando `postTransition`); "Encerrar" = reject/supersede com `reason`/`by`. Deixa explícito que nada é apagado (append-only). → commit `feat(ui): feature management table`.

6. **Rebuild `ui/dist` + verde total** — `npm ci` + `npm run build`; `go vet ./... && go test ./... -count=1`. → commit `chore(ui): rebuild dist with feature authoring`.

## Verificação final

- `go vet`/`go test` verdes; `tsc` limpo.
- Dogfood no clone do hub: criar feature na UI → escrever spec → aprovar (gate passa só com spec real) → supersedir por outra; conferir os commits (`cerne: create…`, doc update, `cerne: supersede…`) e o avanço do `docs.lock` no git. Números no result.md.

## Nota de entrega

Stacked sobre o [case 0011](../0011_ui-read-pages/spec.md) (MR !6). Quando o 0011 mergear na `main`, rebasar este branch na `main`. Reconferir numeração (0012) na entrega, como no 0011.
