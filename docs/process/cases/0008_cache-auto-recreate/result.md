# Resultado — Case 0008 (auto-recuperação do clone de cache)

**Status:** Done (2026-06-13). Suíte: 342 testes verdes, `go vet` limpo.

## O que foi entregue

`internal/cli/context.go` `resolvePaths`: quando `<cache>/repo/.git` falta, em vez de abortar com "run 'cerne init'", a função **recria o clone** do `backend` gravado no `.cernerc` (`recreateCache`: limpa parcial → `MkdirAll(cacheDir)` → aviso em **stderr** → `gitback.Clone`). Como `resolvePaths` é o ponto único por onde todos os comandos passam, status/pull/lint/search/compile/classify/push ficam auto-recuperáveis. Mensagem de falha do `resolveProject` (pull) atualizada de "rode init" para "apague o diretório e rode o comando novamente".

O impasse some por construção: `init` segue (corretamente) recusando re-inicialização com `.cernerc` existente, mas o cache não depende mais disso.

## Verificação

- **Teste** (`internal/cli/context_test.go` `TestResolvePathsRecreatesMissingCache`): projeto inicializado + feature pushada, `RemoveAll(<cache>/repo)`, `cerne status` ⇒ recupera a feature e o `.git` é recriado. (Vermelho antes: "cached backend clone missing"; verde depois.)
- **Dogfood no hub** (clone bare local): após apagar `<cache>/hub-0008/repo`, `cerne status` imprimiu em stderr `clone de cache ausente — recriando de ...` e listou o status normalmente; `.git` recriado.

## Decisão (ver spec)

Auto-recreate em `resolvePaths` (não `init`), pois é o caminho comum a todos os comandos. Aviso em stderr para não poluir stdout de `--json`. `index.db` é reconstruído pelo fluxo existente (pull/search). Descartado: `init --recreate-cache` (flag para um problema que não devia existir).

## Inventário

- **Código:** `internal/cli/context.go` (`recreateCache` + auto-recreate em `resolvePaths`; mensagem do `resolveProject` atualizada).
- **Testes:** `internal/cli/context_test.go` (novo; +1; 341→342).
- **Commit:** `fix(cli): recreate the cache clone from .cernerc when missing` + docs deste case.

## Fora de escopo

- Detecção de corrupção parcial do cache além de `.git` ausente.
- `init --recreate-cache`.
