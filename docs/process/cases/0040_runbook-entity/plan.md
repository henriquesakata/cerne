# Case 0040 — Plano: runbook como entidade

**Spec:** [spec.md](spec.md) — fonte dos critérios de aceitação.
**Branch:** `claude/runbook-entity-f0040` (worktree).
**Status:** Rascunho (2026-06-21).

## Estratégia

MR único, TDD, espelhando a entidade **decisão** camada por camada (FSM de 2 estados é o molde mínimo) e adicionando o template `runtmpl` da família. Diferente dos casos F-0036..F-0039 (templates de docs de entidades existentes), aqui há uma entidade nova no modelo de dados, então toca domain/store/app/cli/materialize/importer/push/lint/index. Ver [spec.md](spec.md) para o desenho.

## Sequência de tarefas

1. **Template `internal/runtmpl`.** Criar `runtmpl.go` + `_test.go` (Sections, Body, Stub; teste de ordem + stub). Verificação: `go test ./internal/runtmpl/` — critério 2.
2. **Entidade (domain + store).** `internal/domain/runbook.go` (FSM); `internal/store/entity.go` (struct + CRUD) e `layout.go` (`idSubdirs["RB"]` + dirs). Verificação: `go build`, `go test ./internal/store/ ./internal/domain/` — base dos critérios 1 e 4.
3. **app + cli.** `internal/app/runbook.go` (Create/Transition/stub); `internal/cli/runbookcmd.go` (new/draft/substituir) + `root.go`. Teste: `internal/cli/runbookcmd_test.go`. Verificação: `go test ./internal/cli/ -run Runbook` — critérios 1, 3, 4.
4. **Projeção e busca.** `materialize` (seção CONTEXT + `.docs/current/runbooks/`), `importer` (`scanRunbooks` + apply, sai de referenceDirs), `pushcmd` (`backendRelPath`), `lint` (órfãos + needles), `index` (busca). Atualizar testes de importer/materialize. Verificação: `go test ./...` — critérios 5 e 6.
5. **e2e + skill + referências.** `e2e/runbook_test.go` (golden path); seção "Runbooks" na skill; `cerne reference new` para as fontes. Commit final: `docs(cerne): complete F-0040 (runbook entity) — case + references`.

## Dependências e ordem

Sequencial: 1 → 2 → 3 → 4 → 5. Tarefa 3+ depende de 1 e 2. Tarefa 5 (refs) depende da feature existir no backend (import). Sem dependências externas.

## Riscos e rollback

- **Risco:** mudar o importer (`docs/runbooks` → runbook) quebra expectativas de teste e a importação do hub. Detecção: suíte de importer. Mitigação: testdata atualizado; migração do hub é follow-up documentado.
- **Risco:** colisão da heurística de estado do importer (`status…superseded`) na spec deste caso. Mitigação: a spec evita "status" + "superseded" nas primeiras 40 linhas (usa "estado"/"substituído").
- **Rollback:** cada tarefa é um commit isolado; `git revert` por tarefa. Nada destrutivo.

## Restrições para o agente

- Espelhar a entidade **decisão** (não inventar um FSM novo).
- NÃO duplicar o marcador de ambiguidade — reusar `spectmpl.AmbiguityMarker`.
- Strings de usuário em pt-BR; código/comentários em inglês.
- Servidor/UI fora de escopo (decisão fechada na spec).

## Definição de pronto

Checklist binário (evidência `arquivo:linha`):
- [ATENDIDO] Crit. 1 — `internal/app/runbook.go:CreateRunbook` + `writeRunbookStub`; `internal/cli/runbookcmd_test.go:TestRunbookNew`, `internal/runtmpl/runtmpl_test.go:TestStubReadsAsStub`.
- [ATENDIDO] Crit. 2 — `internal/runtmpl/runtmpl.go`; `runtmpl_test.go:TestSectionsCanonical`.
- [ATENDIDO] Crit. 3 — `internal/cli/runbookcmd.go:BuildRunbookDraftPrompt`; `runbookcmd_test.go:TestBuildRunbookDraftPrompt`.
- [ATENDIDO] Crit. 4 — `internal/domain/runbook.go`; `domain/runbook_test.go`, `runbookcmd_test.go:TestRunbookSubstituir`.
- [ATENDIDO] Crit. 5 — `materialize`/`lint`/`index` atualizados; `e2e/runbook_test.go:TestRunbookGoldenPath` (materializa + CONTEXT).
- [ATENDIDO] Crit. 6 — `internal/importer/importer.go:scanRunbooks`; `importer_test.go:TestScanHubTree`/`TestApplyHubTree`.
- [ATENDIDO] Crit. 7 — referências registradas (`--informs F-0040`).
- [ATENDIDO] Suíte completa: `go vet ./...` limpo, `go test ./...` = 587 passed em 28 pacotes.
- [PENDENTE] result.md + `cerne feature complete`.
