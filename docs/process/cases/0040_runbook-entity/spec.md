# Case 0040 — Runbook como entidade de primeira classe

tags: #spec #cerne #domain #cli #runtmpl #docs

**Status:** Rascunho (2026-06-21) — spec para revisão.
**Tipo:** Novo recurso — adiciona um tipo de entidade ao modelo de dados do cerne.
**Origem:** decisão do usuário (2026-06-21): "fazer o mesmo trabalho para o runbook também", após padronizar spec (F-0036), plano (F-0037), ADR (F-0038) e result (F-0039).

## Problema

Runbook (procedimento operacional: deploy, smoke, recuperação, migração, setup) não é entidade no cerne. O modelo tem apenas features, decisões e referências; o importer mapeia `docs/runbooks/*.md` para **referências** (`internal/importer/importer.go`, referenceDirs). O hub tem 39 runbooks sem estrutura canônica, com lacunas (sem "quando usar" em ~15, sem rollback em ~25, sem escalonamento em ~30). A família de templates (spec/plano/result/ADR) padronizou os docs de feature e decisão; o runbook ficou de fora, sem template, sem comando, sem ciclo próprio.

## Objetivo e critérios de aceitação

Tornar o runbook uma entidade de primeira classe, espelhando a decisão (FSM simples), com template canônico — fechando a lacuna operacional.

1. QUANDO `cerne runbook new "<título>"` roda O SISTEMA DEVE mintar `RB-NNNN` ativo, persistir a entidade e materializar `content/runbooks/RB-NNNN.md` do template canônico; o scaffold DEVE ser lido como stub (`doccheck.HasRealContent` falso).
2. O pacote `internal/runtmpl` DEVE expor as seções canônicas em ordem — `Quando usar`, `Pré-requisitos`, `Procedimento`, `Verificação`, `Rollback e troubleshooting` — e `Stub(title)`, com teste fixando a ordem.
3. QUANDO `cerne runbook draft <id>` roda O SISTEMA DEVE montar um prompt pedindo as 5 seções e as regras: passos com saída esperada, escalonamento, marcador `[ESCLARECER:`, right-sizing.
4. QUANDO `cerne runbook substituir <id> --by <id> --reason` roda O SISTEMA DEVE mover o ciclo de ativo para substituído (FSM em `internal/domain/runbook.go`).
5. Runbooks ativos DEVEM materializar em `.docs/current/runbooks/` e na seção `## Runbooks` do `CONTEXT.md`, ser indexados (`cerne search`/`ask`) e cobertos pelo `cerne lint` (órfãos + menção a substituído).
6. O importer DEVE mapear `docs/runbooks/*.md` para entidades runbook (antes: referências).
7. As fontes externas que embasaram o template DEVEM estar registradas como referências, com `--informs` apontando para esta feature.

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| Modelar runbook | Entidade nova de primeira classe (não reusar referência) | Referência é proveniência externa (URL); runbook é procedimento autorado com ciclo próprio. |
| Ciclo de vida | ativo → substituído, via `substituir` (espelha a decisão) | Runbook fica obsoleto/é trocado por outro; o ciclo mais simples que serve. Sem gate de aprovação. |
| Prefixo de id | `RB-NNNN` | "R" já é referência; `NextID` é genérico (mapa idSubdirs). |
| Template | 5 seções, do gatilho ao rollback | Espelha a família; cobre as lacunas do hub (quando usar, verificação, rollback, escalonamento). |
| Importer | `docs/runbooks/` vira runbook (sai de referenceDirs) | O destino correto; runbooks deixam de ser referências. Migração dos runbooks-referência já existentes no hub é follow-up. |
| Decisão arquitetural | Registrada como ADR no backend (dogfood do template de ADR) | Adicionar entidade muda o modelo de dados; merece registro. |

## Desenho

Espelha a entidade decisão, camada por camada:

- `internal/domain/runbook.go` — `RunbookState` (active/superseded) + `TransitionRunbook`.
- `internal/store/entity.go` + `layout.go` — `Runbook`, `runbooksSubdir`, CRUD, `idSubdirs["RB"]`, dirs de layout.
- `internal/app/runbook.go` — `CreateRunbook`, `TransitionRunbook`, `writeRunbookStub`.
- `internal/cli/runbookcmd.go` — `cerne runbook new|draft|substituir` (+ `root.go`).
- `internal/runtmpl/runtmpl.go` — template canônico.
- `internal/materialize` — runbooks ativos em `.docs/current/runbooks/` + seção CONTEXT.
- `internal/importer` — `scanRunbooks` + apply; `internal/cli/pushcmd.go` — `backendRelPath`.
- `internal/lint`, `internal/index` — paridade (órfãos, menção a substituído, busca).

## Verificação

Plano de testes (TDD), 1:1 com os critérios: `internal/domain/runbook_test.go`, `internal/runtmpl/runtmpl_test.go`, `internal/cli/runbookcmd_test.go`, `e2e/runbook_test.go` (golden path), e os testes de importer/materialize atualizados. Suíte completa verde.

## Fora de escopo

- Superfície de runbook no servidor/UI (browsing read-only) — follow-up.
- Migração dos runbooks-referência já importados no hub — follow-up.
- Regra de `cerne lint` para completude das seções do runbook.
