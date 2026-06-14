# Plano — Case 0018 (import auto-classifica módulos)

**Goal:** `cerne import` roda o backfill de classificação (apply no `--yes`, projeção no dry-run), mesmo sem candidatos novos, e reporta o resíduo sem módulo — para um subsistema declarado depois (ex.: studio) não ficar órfão da wiki.

## Tarefa única (TDD)

1. **Regressão primeiro** (`internal/cli/importcmd_test.go`): cenário do studio —
   - backend com módulos SEM `finance`; importar (`--yes`) um root case (`docs/process/cases/NNNN/`) cujo spec casa "finance" → feature fica sem módulo.
   - declarar `finance` no projeto (reescrever project.json via store) ;
   - `import --yes` de novo (nada novo) → assert `LoadFeature(...).Module == "finance"` e saída contém "Classificadas 1".
   - dry-run: com feature sem módulo classificável, `import` (sem --yes) imprime "classificaria"/"permaneceriam" e NÃO cria commit (snapshot inalterado).

2. **Implementar** (`internal/cli/importcmd.go` `runImport`): remover o early-return cego; nos dois modos rodar `app.ClassifyBackfill` (apply=false no dry-run com projDir=""; apply=true no --yes) após o fluxo de import; imprimir classificadas + resíduo (pt-BR). Importar `app` já está no arquivo.

Verificar: `go vet` + `go test ./...` verdes. Smoke opcional: `cerne import --yes` no hub → 0 classificadas novas (studio já corrigido), resíduo reportado.

## Fechamento

- `result.md`; status spec → Done; linha 0018 no índice `docs/process/CLAUDE.md`.
- `scripts/update-cerne.ps1` para o binário do PATH.
