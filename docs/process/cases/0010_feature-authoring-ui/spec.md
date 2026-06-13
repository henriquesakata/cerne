# Case 0010 — Gestão e autoria de features na UI

tags: #spec #cerne #ui #server #app

**Status:** Spec (rascunho — pré-plan.md, 2026-06-13)
**Tipo:** Autoria (criar feature + editar spec/plan/result na UI) + superfície de gestão por estado, com 2 operações de escrita novas no servidor, reusando o core compartilhado em `internal/app`.
**Origem:** Pedido do usuário: uma página para **cadastrar/editar/remover** features, com controle maior sobre as criadas/aprovadas/pendentes/rejeitadas — "a feature que gera a spec". **Depende do [case 0009](../0009_ui-read-pages/spec.md)** (shell de navegação): a página de gestão é uma entrada da nav.

---

## Problema

A UI hoje **transiciona** features mas não consegue:

1. **Criar** uma feature. Só existe `cerne feature new` na CLI ([`featurecmd.go`](../../../../internal/cli/featurecmd.go)): `NextID` → `SaveFeature` (candidate) → stub de spec → evento `feature.created` → commit/push/lock. O navegador não tem esse caminho.
2. **Escrever/editar a spec** (nem plan/result). O servidor só sabe gravar `content/artifacts/<id>/` (diagramas, via `PUT /api/artifacts`); os docs de ciclo de vida são read-only na UI. **"A feature que gera a spec"**: a feature nasce candidate com um **stub** de spec, e o gate de `approve` exige spec **real** (não-stub) — escrever a spec é o passo crítico que falta, e hoje só dá pra fazer editando arquivos no disco + `cerne pull`/`push`.
3. **Gerir por estado num só lugar**. O kanban é um board de fluxo; falta uma superfície densa (tabela, filtros por estado, ações inline) pra "ter o controle maior" pedido.

Sobre **"remover"**: por decisão deste case (alinhada ao princípio append-only do cerne — histórico nunca some), remover = **transição de ciclo de vida** (`reject` para candidata, `supersede` para estados posteriores). **Não há hard-delete**; a página torna essas transições legíveis e alcançáveis, sem inventar deleção destrutiva.

---

## Objetivo e critérios de sucesso

Fechar o loop de autoria na UI (criar → escrever spec → aprovar → ... → encerrar) e dar uma superfície de gestão por estado, mantendo o backend como fonte de verdade e o modelo append-only intacto.

**Critério de aceite:**

1. **Criar feature**: formulário "Nova feature" (título + módulo quando o projeto declara módulos) → `POST /api/features` cria uma candidate com stub de spec, espelhando **exatamente** `cerne feature new` (`NextID`, evento `feature.created`, commit+push, atualiza lock). Ao criar, navega para a página da nova feature.
2. **Editar docs**: na página da feature, as abas spec/plan/result ganham modo de edição (editar → salvar) → `PUT /api/features/{id}/docs/{doc}` grava `content/features/<id>/<doc>.md`, commit+push, atualiza lock. O gate de `approve` passa assim que a spec tiver conteúdo real. Stub vs real é sinalizado.
3. **Validação de módulo** espelha a CLI (`validateModule`): com ≥2 módulos declarados `--module`/campo é obrigatório; módulo informado precisa existir.
4. **Gestão por estado**: uma view de gestão (tabela) listando features em **todos** os estados com filtros rápidos (candidate/approved/in_development/implemented/rejected/superseded) e as ações de ciclo de vida inline (aprovar/iniciar/concluir/rejeitar/supersedir) — o "controle maior", complementando o kanban.
5. **"Remover" = reject/supersede**: surge como a afordância de encerramento, com `reason` (e `by` no supersede) obrigatórios — mesmas regras de `ValidateTransitionFlags`. Não existe rota de DELETE; o texto deixa claro que nada é apagado (é substituído/rejeitado, histórico preservado).
6. Mesmo contrato de erro pt-BR; falhas de gate (ex.: aprovar com spec ainda stub) mostram a **mesma** mensagem que a CLI dá. Suíte verde, `go vet` limpo, `ui/dist` reconstruído.

---

## Decisões do design (2026-06-13)

| Fork | Decisão | Razão |
|---|---|---|
| **Criar** | `POST /api/features {title, module}` reusa um core novo `app.CreateFeature(repoDir, projDir, title, module)` extraído de `runFeatureNew` | Fonte única de verdade CLI+servidor; o core de transição já mora em `app` — o de criação deve morar junto |
| **Editar doc** | `PUT /api/features/{id}/docs/{doc}` (`doc ∈ spec\|plan\|result`) grava via `app.SaveFeatureDoc`, com o mesmo rabo commit+snapshot de `SaveArtifact` | Simetria com `SaveArtifact`; os gates já leem esses caminhos (`FeatureDocSet`) |
| **Editor** | `<textarea>` + preview markdown (reusa `ReactMarkdown` já presente), sem editor pesado | Superfície mínima; a spec é markdown puro |
| **"Remover"** | Só reject/supersede; a UI de gestão chama isso de "Encerrar". **Sem** rota DELETE | Append-only é o invariante fundador (current vs history) |
| **Gestão vs kanban** | Rota separada (tabela, filtro por estado, ações inline); o kanban segue board de fluxo | Trabalhos diferentes; o usuário pediu "controle maior" denso |
| **Concorrência** | Escritas são last-writer-wins (igual aos artifacts hoje) | Aceito como comportamento v1; sem locks/merge no escopo |

---

## Desenho

### Core compartilhado (`internal/app`)

```go
// CreateFeature cria uma feature candidate (stub de spec), emite feature.created,
// commita+empurra e atualiza o lock. Extraído de runFeatureNew para CLI e servidor.
func CreateFeature(repoDir, projDir, title, module string) (store.Feature, error)

// SaveFeatureDoc grava content/features/<id>/<doc>.md (doc ∈ spec|plan|result),
// commita+empurra e atualiza o lock (mesmo rabo de SaveArtifact). No-op se idêntico.
func SaveFeatureDoc(repoDir, projDir, id, doc, content string) error
```

`runFeatureNew` passa a chamar `app.CreateFeature` (mantém a mensagem pt-BR da CLI). `validateModule` migra/compartilha com o servidor.

### Servidor — endpoints de escrita novos

```go
// POST /api/features            body {title, module}        → 201 { feature }
// PUT  /api/features/{id}/docs/{doc}  body markdown (text)  → 200 { saved: true }
```

- `POST`: valida título não-vazio e módulo (`validateModule`); erros de validação ⇒ 422 pt-BR (igual ao transition).
- `PUT docs`: valida `doc ∈ {spec,plan,result}` (senão 400); grava via `app.SaveFeatureDoc`.

### Frontend

- **Nova feature**: form (entrada na nav/kanban "Nova feature") → `POST` → redireciona a `#/f/<novo-id>`.
- **Página da feature** ([`FeaturePage.tsx`](../../../../ui/src/FeaturePage.tsx)): abas de doc viram editáveis (toggle editar/salvar, preview); sinaliza stub.
- **Gestão**: nova página/tabela (rota na nav do [case 0009](../0009_ui-read-pages/spec.md)) com filtro por estado e ações inline reusando `postTransition`; "Encerrar" abre o form de reject/supersede.
- **`api.ts`**: `createFeature`, `saveFeatureDoc` + tipos.

---

## Verificação (fluxo completo no navegador)

Contra um clone de teste/hub: criar uma feature na UI → escrever a spec → aprovar (gate passa só com spec real) → supersedir por outra. Conferir no git que os commits (`cerne: create ...`, doc update, `cerne: supersede ...`) entraram e o `docs.lock` avançou a cada passo.

---

## Fora de escopo

- **Hard delete / `git rm`** de entidades (rejeitado por design — append-only).
- Editar docs de decisions/references (o [case 0009](../0009_ui-read-pages/spec.md) as deixa read-only; autoria delas é case futuro).
- Editor rich-text/WYSIWYG, edição colaborativa, resolução de conflito além de last-writer-wins.
- Geração da spec por LLM ("escreva a spec pra mim") — camada LLM do [case 0003](../0003_llm-compile-ask/spec.md).
- Reordenação/priorização do backlog.
