# Plano — Case 0008 (auto-recuperação do clone de cache)

**Goal:** `resolvePaths` recria o clone de cache do backend (`.cernerc`) quando ausente, eliminando o impasse `cached clone missing` vs `init` recusando.

## Tarefas (TDD)

1. **Teste falhando** (`internal/cli/context_test.go` ou um cmd test): inicializa um projeto contra um bare origin local, **apaga `<cache>/repo`**, roda `cerne status` → hoje falha com "cached backend clone missing"; espera-se sucesso (status normal) com o clone recriado. Rodar → ver falhar.
2. **Implementação** (`internal/cli/context.go` `resolvePaths`): quando `repoDir/.git` ausente, em vez de erro: `os.RemoveAll(repoDir)` (limpa parcial), `os.MkdirAll(cacheDir, 0o755)`, aviso pt-BR em **stderr** ("clone de cache ausente — recriando de <backend>…"), `gitback.Clone(cfg.Backend, repoDir)`; erro claro se o clone falhar. Atualizar a mensagem de falha do `resolveProject` (pull) para "apague o diretório e rode o comando novamente".
3. Rodar o teste → passar. `go vet ./...` + `go test ./... -count=1` verdes.
4. Commit `fix(cli): recreate the cache clone from .cernerc when missing`.

## Verificação final

- Dogfood no clone do hub: apagar `<cache>/repo`, rodar `cerne status` → recria e imprime status.
