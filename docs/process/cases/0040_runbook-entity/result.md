# Case 0040 — Resultado: runbook como entidade

**Spec:** [spec.md](spec.md) — fonte dos critérios.
**Data:** 2026-06-21.

## Resultado

Entregue. O runbook virou entidade de primeira classe no cerne (`RB-NNNN`, ciclo ativo/substituído), com template canônico, comandos, projeção e busca — o quinto membro da família de templates e o primeiro tipo de entidade novo desde a fundação.

## Aceitação

Cada critério da spec, binário, com evidência:

1. **ATENDIDO** — `cerne runbook new` minta RB-NNNN ativo + scaffold do template, lido como stub: `internal/app/runbook.go:CreateRunbook`/`writeRunbookStub`; `internal/cli/runbookcmd_test.go:TestRunbookNew`, `internal/runtmpl/runtmpl_test.go:TestStubReadsAsStub`.
2. **ATENDIDO** — 5 seções em ordem + Stub: `internal/runtmpl/runtmpl.go`; `runtmpl_test.go:TestSectionsCanonical`/`TestBodyHasSectionsInOrder`.
3. **ATENDIDO** — `cerne runbook draft`: `internal/cli/runbookcmd.go:BuildRunbookDraftPrompt`; `runbookcmd_test.go:TestBuildRunbookDraftPrompt`/`TestRunbookDraftByoKeyWritesAndCommits`.
4. **ATENDIDO** — FSM ativo→substituído: `internal/domain/runbook.go`; `domain/runbook_test.go`, `runbookcmd_test.go:TestRunbookSubstituir`.
5. **ATENDIDO** — projeção (CONTEXT `## Runbooks` + `.docs/current/runbooks/`), busca e lint: `internal/materialize`, `internal/index`, `internal/lint` atualizados; `e2e/runbook_test.go:TestRunbookGoldenPath`.
6. **ATENDIDO** — importer mapeia `docs/runbooks` → runbook: `internal/importer/importer.go:scanRunbooks`; `importer_test.go:TestScanHubTree`/`TestApplyHubTree`.
7. **ATENDIDO** — referências registradas no backend (`--informs F-0040`).

Suíte: **587 passed em 28 pacotes** (`go test ./... -count=1`); `go vet ./...` limpo.

## O que mudou

- Entidade nova `runbook` em todas as camadas: `internal/domain/runbook.go`, `internal/store/{entity,layout}.go` (prefixo `RB`), `internal/app/runbook.go`.
- Comandos `cerne runbook new|draft|substituir` (`internal/cli/runbookcmd.go`, `root.go`).
- Template `internal/runtmpl` (5 seções, embasado em SRE/PagerDuty).
- Projeção (`materialize`: seção CONTEXT + `.docs/current/runbooks/`), `push` (`backendRelPath`), `importer` (`docs/runbooks` → runbook, fora de referenceDirs), `lint` (órfãos + menção a substituído), `index` (busca/ask).
- Skill `cerne-authoring`: seções "Runbooks" e "Runbook structure".
- A decisão arquitetural está registrada como ADR no backend (dogfood do template de ADR).

## Débitos e follow-ups

- Superfície de runbook no servidor/UI (browsing) — follow-up.
- Migração dos runbooks-referência já importados no hub para entidades runbook — follow-up.
- Regra de `cerne lint` para completude das seções do runbook.

## Aprendizados e desvios

Diferente dos quatro casos anteriores (templates de docs de entidades existentes), este exigiu uma entidade nova — o molde foi a decisão (FSM mínimo de 2 estados), o que tornou o trabalho mecânico em ~9 camadas. `NextID` já era genérico (mapa `idSubdirs`), então o prefixo `RB` entrou sem refactor. Sem desvios da spec; servidor/UI conscientemente fora de escopo.
