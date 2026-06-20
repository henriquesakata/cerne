# Case 0036 — Resultado: lint `stale-candidate`

tags: #result #cerne #lint

**Status:** Implementado — suíte verde (548). Regra aditiva; registro no backend pendente (outward).
**Entregue:** Regra determinística `stale-candidate` em `internal/lint` que sinaliza candidatas há >30 dias sem spec real, com CLI (texto + `--fix-tasks`) e label de UI.

---

## O que mudou

- **[internal/lint/lint.go](../../../../internal/lint/lint.go):** `RuleStaleCandidate` em `RuleOrder`/`ruleRank` (pos 8); `const staleCandidateDays = 30`; `staleCandidateFindings(repoDir, features, now)` (candidate + idade ≥ 30d + sem spec real → finding, reusando `isRealFile`); fio em `Run` com `time.Now()`.
- **[internal/cli/lintcmd.go](../../../../internal/cli/lintcmd.go):** `taskAction` ganha o branch `stale-candidate` → "rejeitar a candidata (cerne feature reject) ou escrever a spec e aprovar".
- **[ui/src/Lint.tsx](../../../../ui/src/Lint.tsx):** label "Candidata parada (sem spec)" + entrada em `RULE_ORDER`. (O `dist` **não** foi recompilado — ver follow-up.)
- **[internal/lint/lint_test.go](../../../../internal/lint/lint_test.go):** `TestStaleCandidateFindings` (pura, `now` fixo, 4 casos) + `TestRunFlagsStaleCandidate` (fio no `Run`, candidate ancião).

## Aceitação

- TDD: testes vermelhos (símbolos indefinidos) → verdes após a regra.
- `go vet ./...` limpo; **`go test ./... = 548 verdes`** (inclui os 2 novos). `TestRunFindings`/`TestLintFindings` seguem verdes sem edição — os seeds usam `CreatedAt=now`, então a regra fica silenciosa (prova de que é aditiva, sem regressão).

## Efeito

- Fecha o flanco aberto pelo caso 0035: candidate era invisível ao lint (`missing-gate-doc` só cobre `approved`+). Agora candidatas registradas-e-abandonadas afloram em `cerne lint`, com tarefa de manutenção pronta (`reject` ou spec+`approve`). O board do registro-no-início se mantém limpo sem entidade nova.

## Fora de escopo / follow-up

- **Rebuild do `ui/dist`:** `Lint.tsx` mudou, mas recompilar aqui re-hasheia ~200 chunks de vendor (diferença de ambiente); rebuildar/commitar o dist num passo limpo no ambiente canônico.
- **3 rules de épico ausentes na UI** (`parent-not-found`/`parent-terminal`/`epic-open-children`): gap pré-existente do `Lint.tsx` — task separada.
- **`gofmt -l`** aponta `internal/cli/root.go` (pré-existente, não tocado).
- **Limiar configurável** e **candidate-com-spec-não-aprovada:** possíveis follow-ups.

## Próximos passos

- Registrar no cerne: `cerne import` + `cerne pull`; commit `docs.lock`.
