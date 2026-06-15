# Plano — Case 0023 (push aceita plan/result net-new)

**Goal:** `cerne push` envia um `plan.md`/`result.md` autorado depois do `spec.md` (feature net-new), sem barrar como "não rastreado"; lixo arbitrário segue barrado.

## Tarefa única (TDD)
`internal/cli/pushcmd.go` + `pushcmd_test.go`:
1. **Regressão primeiro:** teste com feature `F-0001` (spec rastreado no manifest/docs.lock); criar `current/features/F-0001-<slug>/plan.md` com `cerne_id: F-0001`; `cerne push` → sucesso; assert `content/features/F-0001/plan.md` no backend + manifest atualizado. Segundo teste: `notes.md` net-new sob a mesma feature → ainda erro "não rastreado" (mantém o teste existente `TestPush…Untracked`).
2. **Implementar:** helper `acceptableNewDoc(rel, manifest)` (prefixo `current/features/`, base ∈ {plan.md,result.md}, irmão no manifest); em `collectChangedDocs`, ramo `!tracked` aborta só se `!acceptableNewDoc(...)`.

Verificar: `go vet` + `go test ./...` verdes.

## Fechamento
- `result.md`; status spec → Done; índice `docs/process/CLAUDE.md`; `update-cerne.ps1`.
- Smoke: re-`cerne push` do plan.md do F-0311 (hub) → `cerne feature start F-0311`.
