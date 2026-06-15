# Plano — Case 0024

## Tarefa única (TDD)

1. **Teste (falha primeiro)** — `internal/server/multiproject_test.go`:
   - `seedEmptyBackend(t)`: `gitback.InitBare` → origin bare vazio (sem layout).
   - `TestMultiProjectRegisterInitsEmptyBackend`: `PUT /api/projects/proj-new`
     com `{backend, name}` → 200; `GET .../state` → 200 com 0 features;
     `POST .../features` → 201; clone de verificação do origin tem `project.json`.
   - Verificar: `go test ./internal/server/ -run Empty` falha contra o código atual.

2. **Implementação** — `internal/server/multiproject.go`:
   - `handleRegister`: corpo `{backend, name?, modules?}`; `name` vazio → `pid`.
   - `ensureProjectClone(repoDir, projDir, backend, name, modules)`: após o clone,
     `store.LoadProject` → `ErrNotFound` ⇒ `store.InitLayout` + `CommitAndPush`.
   - Import de `internal/store`.
   - Verificar: teste passa; `go vet ./...`; `go test ./...` toda verde.

3. **Fechamento** — `result.md` + índice `docs/process/CLAUDE.md` (0024 Done).
   - Binário: rebuild via `scripts/update-cerne.ps1` (CLI local) + push pro gitlab
     (o build do cerne-service no hub usa o source seeded em `.workspaces/rockt/cerne`).
