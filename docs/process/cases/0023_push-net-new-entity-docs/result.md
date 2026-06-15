# Resultado — Case 0023 (push aceita plan/result net-new)

**Status:** Done (2026-06-14). Suíte: **463 testes** verdes, `go vet` limpo.

## O que foi entregue

`cerne push` deixou de barrar um `plan.md`/`result.md` autorado depois do stub de `spec.md` (fluxo cerne-native pós-`feature new`), desbloqueando `feature start`/`complete` para features net-new.

- **`internal/cli/pushcmd.go`**:
  - `acceptableNewDoc(rel, manifest)` (novo): `true` sse `rel` ∈ `current/features/`, base ∈ {`plan.md`,`result.md`}, e há um irmão no manifest sob o mesmo folder (`spec.md` rastreado prova que a feature existe).
  - `collectChangedDocs`: o ramo `!tracked` agora aborta **só** quando `!acceptableNewDoc(...)`; caso contrário segue (parse `cerne_id` → `backendRelPath` → append). O resto do `runPush` (escrita no backend, `lock.Materialized[rel]=sum`, commit+push) já tratava arquivo novo.
  - Commit `5210fb30`.

## Causa-raiz
`collectChangedDocs` tratava qualquer arquivo ausente do manifest como "não rastreado" e abortava — certo para lixo, mas barrava também o `plan.md` legítimo. `feature new` só stuba `spec.md`; logo, features net-new nunca conseguiam pushar plano/resultado (só as importadas, que já vinham com eles).

## Verificação
| Item | Como | Resultado |
|---|---|---|
| plan net-new pushável | `TestPushNetNewPlan` | `plan.md` sob feature rastreada → escrito no backend + no origin + no manifest |
| lixo ainda barrado | teste existente (`notes.md`) | continua "não rastreado" |
| suíte + vet | `go test ./...` / `go vet` | 463 testes; limpo |
| smoke real | re-`cerne push` do plan.md do F-0311 (hub) | desbloqueia `cerne feature start F-0311` |

## Inventário
- **Go:** `internal/cli/pushcmd.go` (+`acceptableNewDoc`, ajuste em `collectChangedDocs`), `internal/cli/pushcmd_test.go` (+`TestPushNetNewPlan`).
- **Commits:** `9dc1a5f9` (abertura), `5210fb30` (fix) (+ este result).

## Fora de escopo
- Docs net-new de decisão/referência sem stub (nascem via `decision/reference new`).
- Criar entidade nova via push; deleção via push.
