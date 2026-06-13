# Resultado — Case 0011 (autoria nativa de decisions)

**Status:** Implementado e verificado (2026-06-13) — suíte 361 testes verdes, `go vet` limpo. Aguardando revisão/merge.

## O que foi entregue

A decision ganhou a mesma superfície de autoria e transição que a feature já tinha, ligando o FSM e a persistência que já existiam mas estavam sem CLI. O verbo `supersede` virou `substituir` (feature + decision); o dado segue em inglês.

- **`internal/app/decision.go` (novo)** — núcleo compartilhável (CLI + futuro server):
  - `CreateDecision(repoDir, projDir, title)`: `NextID("D")` → entidade `active` → stub `content/decisions/<id>.md` (frontmatter `cerne_id` + placeholder) → evento `decision.created` → `CommitAndSnapshot` (commit+push+lock).
  - `TransitionDecision(repoDir, projDir, id, by, reason)`: valida flags, carrega as duas decisions (erros pt-BR para ausente / `--by` == própria / já terminal), aplica `domain.TransitionDecision` (FSM que antes só os testes exercitavam), persiste `state/superseded_by/reason`, evento `decision.superseded`, sincroniza.
- **`internal/cli/decisioncmd.go` (novo)** + registro em `root.go`:
  - `cerne decision new "<título>"` → "D-NNNN criada (active). Próximo passo: escreva o ADR e rode 'cerne pull'."
  - `cerne decision substituir <id> --by <id> --reason "…"` → "D-NNNN: active → superseded".
- **Rename `feature supersede` → `feature substituir`**: `transitioncmd.go` (verbo + Short pt-BR), `app/transition.go` (mensagens de `ValidateTransitionFlags` "para a substituição"), ajuda dos flags `--by/--reason`. Testes e e2e atualizados.

## Decisões (ver spec)

- **cerne-native, não file-authored**: ADR novo nasce no backend (`content/decisions/`), materializado read-only no `.docs/`, como já é com specs. `import` segue onboarding único (não-idempotente) dos 45 ADRs legados.
- **Dado em inglês**: o valor de estado `superseded`, o campo `superseded_by`, o identificador `domain.ActionSupersede` (= `"supersede"`), os eventos `*.superseded`, a regra de lint `ref-to-superseded` e o **token de ação no wire da UI** (`action: 'supersede'`, com `label: 'Substituir'`) ficaram intactos. Só o verbo do CLI e a prosa pt-BR mudaram. Ver memória `ptbr-formal-correctness`.
- **Decision nasce `active`** (sem `draft`/gate): o stub materializa até o autor preencher (auto-corrige no `pull`).

## Critérios de aceite

| # | Critério | Resultado |
|---|---|---|
| 1 | `decision new` cunha D-NNNN active + stub + `decision.created` + commit/lock | ✅ `TestDecisionNew`, `TestDecisionGoldenPath` |
| 2 | `decision substituir` transiciona + evento + erros (ausente / `--by` inexistente / `--by`==self / já superseded / flags) | ✅ `TestDecisionSubstituir`, `TestDecisionSubstituirFlagRules` |
| 3 | `feature supersede` → `feature substituir` (mesma ação) | ✅ `TestTransitionSubstituir`, smoke `--help`, golden path stage 7 |
| 4 | Dado permanece inglês | ✅ varredura `\bsupersede\b` + suíte; `ActionSupersede`/eventos/lint/UI-wire inalterados |
| 5 | Downstream sem mudança: materialize some com a decision substituída; lint a flagra | ✅ `TestDecisionGoldenPath` (D-0001 sai do `.docs/`); regras de decision em `lint` já cobertas |
| 6 | `go vet` + suíte + e2e verdes | ✅ 361 testes, vet limpo |

## Inventário

- **Código novo:** `internal/app/decision.go`, `internal/cli/decisioncmd.go`.
- **Código alterado:** `internal/cli/root.go` (registro), `internal/cli/transitioncmd.go` (verbo + ajuda), `internal/app/transition.go` (mensagens pt-BR).
- **Testes:** `internal/cli/decisioncmd_test.go` (novo: 3 funções), `e2e/decision_test.go` (novo: golden path da decision), `internal/cli/transitioncmd_test.go` + `e2e/golden_path_test.go` (rename).
- **Docs:** este case (spec/plan/result) + linha no `docs/process/CLAUDE.md`.

## Follow-ups (fora de escopo, ver spec)

- **Import incremental** (dedup por `imported_from`) — re-importar só o ADR novo; hoje `import` é onboarding único.
- **Gate de conteúdo / estado `draft`** para decision (hoje nasce `active`; stub materializa transitoriamente).
- **Migração dos 45 ADRs legados** para autoria nativa — seguem em `docs/architecture/adr/`.
- **UI**: criar/substituir decision pela UI (o núcleo `internal/app` já está pronto).
- **Demais verbos** (approve/reject/start/complete) seguem em inglês — escopo escolhido foi só `substituir`.
