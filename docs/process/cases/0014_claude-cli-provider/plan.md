# Plano — Case 0014 (provider claude-cli + `cerne spec draft`)

**Goal:** Usar a assinatura Claude Code (sem API key) via provider `claude-cli`, e um comando `cerne spec draft` que rascunha a spec de uma feature.

## Tarefas (TDD, uma por commit)

1. **Config `claude_bin`** (`internal/project/config.go`): `LLMConfig.ClaudeBin` (toml `claude_bin`, default `"claude"` em `WithDefaults`). Teste: round-trip + default. → commit `feat(project): claude_bin config for the claude-cli provider`.
2. **Provider `claude-cli`** (`internal/llm/cli_provider.go` + teste): `cliProvider{bin, run}` com `run` injetável (default `execRun` via `exec.CommandContext`, `-p`+`--output-format text`, erro com stderr em exit≠0). `NewProvider` roteia: delegation→ErrNoProviderForDelegation, byo-key→http, claude-cli→cli (sem ler chave). Teste com runner fake: Complete devolve stdout; exit≠0 → erro; NewProvider(claude-cli) não exige env de chave. → commit `feat(llm): claude-cli provider (uses the Claude Code subscription)`.
3. **`cerne spec draft <id>`** (`internal/cli/speccmd.go` + teste): parent `spec` + sub `draft`; `BuildSpecDraftPrompt(feature, content)`; delegação imprime prompt; byo-key/claude-cli geram e escrevem `content/features/<id>/spec.md` (frontmatter cerne_id carimbado) + commit+push. Registrar em `root.go`. Teste: `BuildSpecDraftPrompt` inclui id/título/módulo; `spec draft` byo-key (httptest) escreve a spec + commita + lock atualizado; feature inexistente → erro. → commit `feat(cli): cerne spec draft — AI-draft a feature's spec`.

## Verificação final

- `go vet ./...` + `go test ./... -count=1` verdes; modos delegation/byo-key inalterados.
- Smoke real: `claude` no PATH + logado; projeto temp `mode="claude-cli"`; `cerne spec draft F-0001` gera spec real pela assinatura. (Eu rodo este smoke.)
