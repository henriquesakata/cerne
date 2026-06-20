# Case 0039 — Plano: template de result (restmpl)

**Spec:** [spec.md](spec.md) — fonte dos critérios de aceitação.
**Branch:** `claude/result-template-f0039` (worktree).
**Status:** Rascunho (2026-06-20).

## Estratégia

MR único, TDD estrito, espelhando F-0036/F-0037/F-0038 arquivo por arquivo. O result é o quarto membro da família `spectmpl`/`plantmpl`/`dectmpl`: o pacote `restmpl` define a estrutura; `result draft` e o scaffolding no `feature start` a entregam; a skill a documenta. Fecha o loop spec → plano → result. Fatiado em 5 tarefas pequenas, cada uma verde antes do commit.

## Sequência de tarefas

1. **Pacote `internal/restmpl`.** Arquivos: criar `internal/restmpl/restmpl.go`, `restmpl_test.go`. Teste primeiro: `TestSectionsCanonical`, `TestBodyHasSectionsInOrder`, `TestBodyHasGuidanceMarkers`, `TestStub`, `TestStubReadsAsStub`. Implementação: `Sections`, `Body`, `Stub`. Verificação: `go test ./internal/restmpl/` verde — satisfaz critérios 2 e (stub) 1. Commit: `feat(restmpl): canonical result template scaffolded by cerne`.
2. **Scaffold do result no start.** Arquivos: editar `internal/app/feature.go` (`writeResultStub`), `internal/app/transition.go` (chamar em `ActionStart`), `internal/cli/transitioncmd.go` (hint pós-start). Teste primeiro: `internal/cli/transitioncmd_test.go:TestStartScaffoldsResultStub`. Verificação: `go test ./internal/cli/ ./internal/app/` verde, incl. `TestTransitionFullLifecycle` (complete segue barrado) — satisfaz critério 1. Commit: `feat(feature): scaffold result stub on start`.
3. **Comando `cerne result draft`.** Arquivos: criar `internal/cli/resultcmd.go` (+ `resultcmd_test.go`); editar `internal/cli/root.go`. Teste primeiro: `TestBuildResultDraftPrompt`, `TestResultDraftByoKeyWritesAndCommits`, `TestResultDraftUnknownFeature`. Implementação: `newResultCmd`, `BuildResultDraftPrompt` (injeta spec+plano). Verificação: `go test ./internal/cli/ -run Result` verde — satisfaz critério 3. Commit: `feat(resultcmd): cerne result draft aligned to the result template`.
4. **Documentar na skill.** Arquivos: editar `internal/skills/cerne-authoring/SKILL.md` ("Result structure" + passo Result). Verificação: suíte completa `go vet ./... && go test ./... -count=1` verde — satisfaz critério 4. Commit: `docs(skill): document the result structure`.
5. **Registrar as referências externas.** `cerne reference new` para as fontes (`--informs` esta feature). Verificação: `cerne ask "como escrever um result"` cita as fontes — satisfaz critério 5. Commit: `docs(cerne): complete F-0039 (result template) — case + references (docs.lock)`.

## Dependências e ordem

Sequencial: 1 → 2/3 (ambas dependem de `restmpl`; entre si são [paralelas]) → 4 → 5. Tarefa 5 depende desta feature existir no backend (import). Sem dependências externas.

## Riscos e rollback

- **Risco:** o scaffold no start afrouxar o gate de complete. Detecção: `TestTransitionFullLifecycle` + `TestStubReadsAsStub`. Mitigação: `Body` só headings + comentários, lido como stub pelo `doccheck`.
- **Risco:** importer marcar a feature como superseded por colisão de heurística status/superseded (bug visto na F-0038). Mitigação: a spec evita "status…supersed" nas primeiras 40 linhas. (Fix do importer rastreado à parte.)
- **Rollback:** cada tarefa é um commit isolado; `git revert` por tarefa. Nada destrutivo.

## Restrições para o agente

- NÃO duplicar o marcador de ambiguidade — reusar `spectmpl.AmbiguityMarker`.
- NÃO inflar o template — 5 seções enxutas (decisão fechada na spec).
- Strings de usuário em pt-BR; código/comentários em inglês.

## Definição de pronto

Checklist binário (evidência `arquivo:linha`):
- [ATENDIDO] Crit. 1 — scaffold no start + stub: `internal/app/transition.go` (`ActionStart`→`writeResultStub`), `internal/cli/transitioncmd_test.go:TestStartScaffoldsResultStub`, `internal/restmpl/restmpl_test.go:TestStubReadsAsStub`.
- [ATENDIDO] Crit. 2 — seções canônicas: `internal/restmpl/restmpl.go`, `restmpl_test.go:TestSectionsCanonical`.
- [ATENDIDO] Crit. 3 — `result draft`: `internal/cli/resultcmd.go`, `resultcmd_test.go:TestBuildResultDraftPrompt`.
- [ATENDIDO] Crit. 4 — skill: `internal/skills/cerne-authoring/SKILL.md` ("Result structure").
- [ATENDIDO] Crit. 5 — referências: `cerne reference new --informs <id>`.
- [ATENDIDO] Suíte completa verde: `go vet ./...` limpo, `go test ./...` = 572 passed em 27 pacotes.
- [PENDENTE] result.md escrito e `cerne feature complete`.
