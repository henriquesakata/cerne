# Case 0037 — Plano: template de plano (plantmpl)

**Spec:** [spec.md](spec.md) — fonte dos critérios de aceitação.
**Branch:** `claude/dazzling-nobel-2f2271` (worktree).
**Status:** Rascunho (2026-06-20).

## Estratégia

MR único, TDD estrito, espelhando F-0036 arquivo por arquivo. O plano é a contraparte de execução da spec: o pacote `plantmpl` (gêmeo de `spectmpl`) define a estrutura; o comando `plan draft` e o scaffolding no `approve` a entregam; a skill a documenta. Sem repetir a spec — ver [spec.md](spec.md) para o desenho. Fatiado em 5 tarefas pequenas, cada uma verde antes do commit.

## Sequência de tarefas

1. **Pacote `internal/plantmpl`.** Arquivos: criar `internal/plantmpl/plantmpl.go`, `internal/plantmpl/plantmpl_test.go`. Teste primeiro: `TestSectionsCanonical`, `TestBodyHasSectionsInOrder`, `TestBodyHasGuidanceMarkers`, `TestStub`, `TestStubReadsAsStub`. Implementação mínima: `Sections`, `Body`, `Stub`. Verificação: `go test ./internal/plantmpl/` verde — satisfaz critérios 2 e (stub) 1. Commit: `feat(plantmpl): canonical plan template scaffolded by cerne`.
2. **Comando `cerne plan draft`.** Arquivos: criar `internal/cli/plancmd.go`, `internal/cli/plancmd_test.go`; editar `internal/cli/root.go`. Teste primeiro: `TestBuildPlanDraftPrompt`, `TestPlanDraftByoKeyWritesAndCommits`, `TestPlanDraftUnknownFeature`. Implementação: `newPlanCmd`, `BuildPlanDraftPrompt` (injeta a spec como contexto), `writePlanDraft`. Verificação: `go test ./internal/cli/ -run Plan` verde — satisfaz critério 3. Commit: `feat(plancmd): cerne plan draft aligned to the plan template`.
3. **Scaffold do plano no approve.** Arquivos: editar `internal/app/feature.go` (`writePlanStub`, no-op se já há plano real), `internal/app/transition.go` (chamar em `ActionApprove`), `internal/cli/transitioncmd.go` (hint pós-approve). Teste primeiro: `TestApproveScaffoldsPlanStub`. Verificação: `go test ./internal/cli/ ./internal/app/` verde, incluindo o `TestTransitionFullLifecycle` pré-existente (start segue barrado) — satisfaz critério 1. Commit: `feat(feature): scaffold plan stub on approve`.
4. **Documentar na skill.** Arquivos: editar `internal/skills/cerne-authoring/SKILL.md` (seção "Plan structure" + passo Plan). Verificação: suíte completa `go vet ./... && go test ./... -count=1` verde — satisfaz critério 4. Commit: `docs(skill): document the plan structure`.
5. **Registrar as referências externas.** `cerne reference new` para as 7 fontes (`--informs` esta feature). Verificação: `cerne ask "como estruturar um plano"` cita as fontes — satisfaz critério 5. Commit: `docs(cerne): register plan-template references (docs.lock)`.

## Dependências e ordem

Sequencial: 1 → 2/3 (ambas dependem de `plantmpl`; entre si são [paralelas]) → 4 → 5. Tarefa 5 depende desta feature existir no backend (import). Sem dependências externas (sem migração, sem serviço).

## Riscos e rollback

- **Risco:** o scaffold no approve quebrar o gate de start (plano stub contar como real). Detecção: `TestTransitionFullLifecycle` + `TestStubReadsAsStub`. Mitigação: `Body` é só headings + comentários HTML, que o `doccheck` trata como scaffold.
- **Risco:** push go-git via SSH falhar (caveat conhecido neste mac). Detecção: erro no `cerne reference new`. Mitigação: ssh-agent com chave carregada; fallback bare-bridge local → `git push`.
- **Rollback:** cada tarefa é um commit isolado; `git revert` por tarefa. Nada destrutivo ou irreversível.

## Restrições para o agente

- NÃO alterar `internal/spectmpl` além de reusar `AmbiguityMarker` (sem duplicar o marcador).
- NÃO adicionar seções novas ao template por tipo de trabalho — right-sizing por colapso de seção (decisão fechada na spec).
- Respeitar a convenção do repo: strings de usuário em pt-BR, código/comentários em inglês.

## Definição de pronto

Checklist binário (evidência `arquivo:linha`):
- [ATENDIDO] Crit. 1 — scaffold no approve + stub fechado: `internal/app/transition.go` (`ActionApprove`→`writePlanStub`), `internal/cli/transitioncmd_test.go:TestApproveScaffoldsPlanStub`, `internal/plantmpl/plantmpl_test.go:TestStubReadsAsStub`.
- [ATENDIDO] Crit. 2 — seções canônicas: `internal/plantmpl/plantmpl.go`, `plantmpl_test.go:TestSectionsCanonical`.
- [ATENDIDO] Crit. 3 — `plan draft`: `internal/cli/plancmd.go`, `plancmd_test.go:TestBuildPlanDraftPrompt`.
- [ATENDIDO] Crit. 4 — skill: `internal/skills/cerne-authoring/SKILL.md` (seção "Plan structure").
- [ATENDIDO] Crit. 5 — referências: 7 `cerne reference new --informs <id>`.
- [ATENDIDO] Suíte completa verde: `go vet ./...` limpo, `go test ./...` = 555 passed em 25 pacotes.
- [PENDENTE] result.md escrito e `cerne feature complete`.
