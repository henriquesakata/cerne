# Plano — Case 0014 (autoria nativa de decisions)

**Goal:** `cerne decision new`/`decision substituir` ligando o FSM/persistência de decision já existentes; renomear `feature supersede` → `feature substituir`. Dado (`superseded`, `superseded_by`, `ActionSupersede`, `*.superseded`, `ref-to-superseded`) fica em inglês.

Tudo em TDD: teste vermelho → implementação mínima → verde. As funções de `internal/app` que sincronizam o backend (chamam `CommitAndSnapshot`) são cobertas **via CLI** com `setupProject` (origin git real), como já é com `TransitionFeature` — não há teste de git no pacote `app`.

## Tarefas

1. **`internal/app/decision.go` — núcleo** (sem teste próprio; coberto pelas tarefas 2–3 via CLI):
   - `CreateDecision(repoDir, projDir, title) (store.Decision, error)`: `NextID("D")` → `store.Decision{State: domain.DecisionActive, CreatedAt/UpdatedAt: now}` → `SaveDecision` → stub `content/decisions/<id>.md` (`---\ncerne_id: <id>\n---\n\n# <title>\n\n> Decisão pendente — descreva contexto, decisão e consequências.\n`) → `AppendEvent(NewEvent(id,"decision.created",{title}, EventActor()))` → `CommitAndSnapshot(... "cerne: create <id> (<title>)")`.
   - `DecisionOutcome{ID, From, To domain.DecisionState}`.
   - `TransitionDecision(repoDir, projDir, id, by, reason) (DecisionOutcome, error)`: `ValidateTransitionFlags(domain.ActionSupersede, by, reason)`; `LoadDecision(id)` (erro pt-BR se ausente); `by != id`; `LoadDecision(by)` existe; `domain.TransitionDecision(d.State, ActionSupersede)` (devolve `ErrInvalidTransition` se já `superseded`); seta `State/SupersededBy/Reason/UpdatedAt`; `SaveDecision`; `AppendEvent(... "decision.superseded", {by, reason})`; `CommitAndSnapshot(... "cerne: supersede <id>")`.

2. **`cerne decision new` (CLI)** — TDD `internal/cli/decisioncmd_test.go::TestDecisionNew` (red):
   - `decisioncmd.go`: `newDecisionCmd()` (grupo `decision`, `--dir` oculta) com sub `new <title>` → `app.CreateDecision`; saída `"%s criada (active). Próximo passo: escreva o ADR e rode 'cerne pull'."`. Registrar em `root.go`.
   - Asserções: D-0001 `active`; `content/decisions/D-0001.md` começa com `---\ncerne_id: D-0001\n---` e contém `> Decisão pendente`; evento `decision.created`; `lock.Snapshot == origin HEAD`; título vazio → erro pt-BR. → commit `feat(decision): cerne decision new (native ADR authoring)`.

3. **`cerne decision substituir` (CLI)** — TDD `TestDecisionSubstituir` + `TestDecisionSubstituirRules` (red):
   - `decisioncmd.go`: sub `substituir <id> --by <id> --reason …` → `app.TransitionDecision`; saída `"%s: active → superseded"`.
   - Asserções: sucesso (`state=superseded`, `superseded_by`, `reason`, evento `decision.superseded` com payload `by`/`reason`, lock parity); erros: `--by` ausente (`--by`), `--reason` ausente (`--reason`), id inexistente (nomeia o id), `--by` inexistente (nomeia o `--by`), `--by == id`, decision já `superseded` (`ErrInvalidTransition`). → commit `feat(decision): cerne decision substituir (validated supersede)`.

4. **Rename `feature supersede` → `feature substituir`** — TDD (ajustar testes primeiro, ver vermelho, então renomear):
   - `internal/cli/transitioncmd.go`: spec vira `{"substituir", "Substituir uma feature por outra (requer --by e --reason)", domain.ActionSupersede}`.
   - `internal/app/transition.go`: as 2 mensagens de `ValidateTransitionFlags` "para supersede" → "para a substituição". (`ActionSupersede`, `eventTypes`, mensagem de commit ficam.)
   - Ajustar `internal/cli/transitioncmd_test.go` (`TestTransitionFlagRules`, `TestTransitionSupersede`: arg `"supersede"` → `"substituir"`) e `e2e/golden_path_test.go` stage 7 (`"feature","supersede"` → `"substituir"`). → commit `refactor(cli)!: rename 'feature supersede' to 'feature substituir' (pt-BR)`.

5. **e2e `e2e/decision_test.go`** — golden path da decision: `init → decision new → pull (D-0001 em .docs/current/decisions/, stub) → escreve ADR real + push → decision substituir D-0001 --by D-0002 → pull (some de .docs/)`, com `assertLockParity` em cada etapa e fresh-clone do origin. → commit `test(e2e): decision new → substituir golden path`.

## Verificação final

- `go vet ./...` + `go test ./... -count=1` verdes (lint/materialize de decision já cobertos no pacote, reexercitados no e2e).
- `cerne decision new "X"` → D-NNNN + stub; `cerne decision substituir` move para `superseded` e some do `.docs/`.
- `cerne feature substituir` responde no nome novo; `cerne feature supersede` deixa de existir.
- Atualizar `docs/process/CLAUDE.md` (status do 0014) e escrever `result.md` antes do merge.
