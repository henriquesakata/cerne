# Plano — Case 0009 (provider LLM BYO-key)

**Goal:** Tornar real o modo `byo-key`: provider HTTP (Anthropic Messages API, net/http cru) + caminho síncrono em compile/ask, sem mexer no default delegação.

## Tarefas (TDD, uma por commit)

1. **Config** (`internal/project/config.go`): estender `LLMConfig` com `Provider`, `Model`, `KeyEnv`, `BaseURL`, `MaxTokens` (tags toml `provider`/`model`/`key_env`/`base_url`/`max_tokens`). Helper de defaults (`provider=anthropic`, `model=claude-opus-4-8`, `key_env=CERNE_LLM_API_KEY`, `base_url=https://api.anthropic.com`, `max_tokens=8000`). Teste: round-trip TOML + defaults aplicados quando ausentes; segredo nunca persistido (só `key_env`). → commit `feat(project): byo-key llm config fields`.
2. **Provider HTTP** (`internal/llm`): `NewProvider(cfg)` (lê chave do env `cfg.KeyEnv`, erro se ausente em byo-key) + `httpProvider.Complete` (POST `{base_url}/v1/messages`, headers `x-api-key`/`anthropic-version: 2023-06-01`/`content-type`, body `{model,max_tokens,messages:[{role:"user",content:prompt}]}`, sem temperature/thinking; parse `content[0].text`; erro status+corpo em não-200). Teste com `httptest.Server` (via base_url): assert headers/modelo/mensagem na request, retorna resposta canônica; não-200 → erro; chave ausente → erro. → commit `feat(llm): http provider for the anthropic messages api`.
3. **compile síncrono** (`internal/cli/compilecmd.go` + `internal/compile`): quando `ResolveMode != delegation`, `NewProvider`, para cada página suja `Complete(BuildPagePrompt)`, escreve `content/wiki/<rel>.md` + frontmatter de proveniência (reusa o stamping do `--ingest`), um commit+push + lock. Remover o erro "modo não suportado". Teste byo-key com httptest/fake: `compile --force` escreve+commita a página, sem `.pending`. → commit `feat(cli): synchronous compile for byo-key mode`.
4. **ask síncrono** (`internal/cli/askcmd.go`): quando `ResolveMode != delegation`, `NewProvider`, `Complete(BuildAskPrompt)`, imprime a resposta. Remover o erro "modo não suportado". Teste byo-key com httptest: resposta impressa no stdout. → commit `feat(cli): synchronous ask for byo-key mode`.

## Verificação final

- `go vet ./...` + `go test ./... -count=1` verdes; modo delegação (default) inalterado (testes existentes intactos).
- Smoke determinístico via httptest (sem rede real). Sem nova dependência no go.mod (net/http stdlib).
