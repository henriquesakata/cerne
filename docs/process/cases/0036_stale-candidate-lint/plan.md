# Case 0036 — Plano

tags: #plan #cerne #lint

1. **spec + plan (registro)** — este `spec.md` + `plan.md`.
2. **Teste vermelho** — `TestStaleCandidateFindings` + `TestRunFlagsStaleCandidate` em [lint_test.go](../../../../internal/lint/lint_test.go); roda e falha (símbolos indefinidos). Um commit (TDD).
3. **Regra verde** — [lint.go](../../../../internal/lint/lint.go): const + `RuleOrder`/`ruleRank` + `staleCandidateFindings` + fio no `Run`. Testes verdes. Um commit.
4. **CLI + UI** — `taskAction` (`--fix-tasks`) + `Lint.tsx` (label/order). Suíte verde. Um commit.
5. **result.md** — registra o entregue.
6. **Fechamento** — `cerne import` + `cerne pull`; commit `docs.lock`.

## Notas / riscos

- Determinismo: a regra usa tempo; `now` é injetado na função e `time.Now()` só no `Run`. Os seeds existentes usam `CreatedAt=now` → regra silenciosa → sem regressão.
- `ui/dist`: editar `Lint.tsx` pede rebuild do dist; o rebuild re-hasheia ~200 chunks (ruído de ambiente darwin × Windows canônico), então o dist fica para um passo limpo no ambiente canônico.
- `gofmt -l` aponta `internal/cli/root.go` (pré-existente, não tocado aqui).
