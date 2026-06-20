# Case 0038 — Plano: template de ADR (dectmpl)

**Spec:** [spec.md](spec.md) — fonte dos critérios de aceitação.
**Branch:** `claude/dazzling-nobel-2f2271` (worktree).
**Status:** Rascunho (2026-06-20).

## Estratégia

MR único, TDD estrito, espelhando F-0036/F-0037 arquivo por arquivo. O ADR é a terceira família de `spectmpl`/`plantmpl`: o pacote `dectmpl` define a estrutura; `decision draft` e o scaffolding no `decision new` a entregam; a skill a documenta. Sem repetir a spec — ver [spec.md](spec.md). Fatiado em 5 tarefas pequenas, cada uma verde antes do commit.

## Sequência de tarefas

1. **Pacote `internal/dectmpl`.** Arquivos: criar `internal/dectmpl/dectmpl.go`, `internal/dectmpl/dectmpl_test.go`. Teste primeiro: `TestSectionsCanonical`, `TestBodyHasSectionsInOrder`, `TestBodyHasGuidanceMarkers`, `TestStub`, `TestStubReadsAsStub`. Implementação: `Sections`, `Body`, `Stub`. Verificação: `go test ./internal/dectmpl/` verde — satisfaz critérios 2 e (stub) 1. Commit: `feat(dectmpl): canonical ADR template scaffolded by cerne`.
2. **Scaffold do ADR pelo template + fix doccheck.** Arquivos: editar `internal/app/decision.go` (`writeDecisionStub` → `dectmpl.Stub`), `internal/doccheck/doccheck.go` (`stubPrefixes` += `"> Decisão pendente"`). Verificação: `go test ./internal/app/ ./internal/doccheck/` verde — satisfaz critérios 1 e 4. Commit: `feat(decision): scaffold ADR from the template; doccheck knows its stub`.
3. **Comando `cerne decision draft`.** Arquivos: editar `internal/cli/decisioncmd.go` (+ `decisioncmd_test.go`). Teste primeiro: `TestBuildDecisionDraftPrompt`, `TestDecisionDraftByoKeyWritesAndCommits`, `TestDecisionDraftUnknownDecision`; atualizar `TestDecisionNew` para o novo scaffold. Verificação: `go test ./internal/cli/ -run Decision` verde — satisfaz critério 3. Commit: `feat(decisioncmd): cerne decision draft aligned to the ADR template`.
4. **Documentar na skill.** Arquivos: editar `internal/skills/cerne-authoring/SKILL.md` ("ADR structure" + seção Decisions). Verificação: suíte completa `go vet ./... && go test ./... -count=1` verde (incl. e2e `TestDecisionGoldenPath` atualizado) — satisfaz critério 5. Commit: `docs(skill): document the ADR structure`.
5. **Registrar as referências externas.** `cerne reference new` para as 4 fontes (`--informs` esta feature). Verificação: `cerne ask "como escrever um ADR"` cita as fontes — satisfaz critério 6. Commit: `docs(cerne): complete F-0038 (ADR template) — case + references (docs.lock)`.

## Dependências e ordem

Sequencial: 1 → 2 → 3 → 4 → 5. Tarefa 2 e 3 dependem de `dectmpl` (tarefa 1). Tarefa 5 depende desta feature existir no backend (import). Sem dependências externas.

## Riscos e rollback

- **Risco:** mudar `writeDecisionStub` quebrar testes que asseguravam o stub antigo (`TestDecisionNew`, e2e `TestDecisionGoldenPath`). Detecção: suíte. Mitigação: atualizados para asserir as seções do template. (Materializou na execução — ambos corrigidos.)
- **Risco:** push go-git via SSH. Mitigação: chave no ssh-agent (confirmado funcionando na F-0037).
- **Rollback:** cada tarefa é um commit isolado; `git revert` por tarefa. Nada destrutivo.

## Restrições para o agente

- NÃO criar seção de status no template — o ciclo é dado gerido pelo cerne (decisão fechada na spec).
- NÃO duplicar o marcador de ambiguidade — reusar `spectmpl.AmbiguityMarker`.
- Strings de usuário em pt-BR; código/comentários em inglês.

## Definição de pronto

Checklist binário (evidência `arquivo:linha`):
- [ATENDIDO] Crit. 1 — scaffold + stub: `internal/app/decision.go` (`writeDecisionStub`), `internal/dectmpl/dectmpl_test.go:TestStubReadsAsStub`, `internal/cli/decisioncmd_test.go:TestDecisionNew`.
- [ATENDIDO] Crit. 2 — seções canônicas: `internal/dectmpl/dectmpl.go`, `dectmpl_test.go:TestSectionsCanonical`.
- [ATENDIDO] Crit. 3 — `decision draft`: `internal/cli/decisioncmd.go`, `decisioncmd_test.go:TestBuildDecisionDraftPrompt`.
- [ATENDIDO] Crit. 4 — doccheck: `internal/doccheck/doccheck.go` (`stubPrefixes`).
- [ATENDIDO] Crit. 5 — skill: `internal/skills/cerne-authoring/SKILL.md` ("ADR structure").
- [ATENDIDO] Crit. 6 — referências: 4 `cerne reference new --informs <id>`.
- [ATENDIDO] Suíte completa verde: `go vet ./...` limpo, `go test ./...` = 563 passed em 26 pacotes.
- [PENDENTE] result.md escrito e `cerne feature complete`.
