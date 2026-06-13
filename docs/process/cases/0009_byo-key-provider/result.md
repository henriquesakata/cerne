# Resultado — Case 0009 (provider LLM BYO-key)

**Status:** Done (2026-06-13). Suíte: 357 testes verdes, `go vet` limpo, **zero** dependência nova no `go.mod`.

## O que foi entregue

Modo `byo-key`: `compile`/`ask` chamam a Anthropic Messages API diretamente (HTTP cru, sem SDK), em uma fase. Default segue sendo delegação, intacto.

- **Config** (`internal/project/config.go`): `LLMConfig` ganhou `Provider`/`Model`/`KeyEnv`/`BaseURL`/`MaxTokens` (tags toml) + `WithDefaults()` (anthropic / claude-opus-4-8 / CERNE_LLM_API_KEY / https://api.anthropic.com / 8000). Segredo nunca no `.cernerc` — só o **nome** do env (`key_env`). Commit `38a1f20`.
- **Provider HTTP** (`internal/llm/http.go`): `NewProvider` (lê chave do env; `ErrAPIKeyMissing`/`ErrNoProviderForDelegation`; só provider `anthropic`) + `httpProvider.Complete` — `POST {base_url}/v1/messages`, headers `x-api-key`/`anthropic-version: 2023-06-01`/`content-type`, body `{model,max_tokens,messages}`, **sem** temperature/top_p/top_k/thinking; junta os blocos `text`; erro com status+corpo truncado em não-200. Commit `994b232`.
- **compile síncrono** (`internal/cli/compilecmd.go`): em `mode!=delegation`, para cada página suja `Complete(BuildPagePrompt)`, escreve `content/wiki/<rel>.md` + frontmatter de proveniência (helper `writeStampedPage` compartilhado com o `--ingest`), um commit+push. Commit `9108bc0`.
- **ask síncrono** (`internal/cli/askcmd.go`): em `mode!=delegation`, `Complete(BuildAskPrompt)` e imprime a resposta. Commit `f4217c8`.

## Bug encontrado e corrigido no dogfood

O dogfood do binário real expôs um bug de UX: **`cerne init` gravava um bloco `[llm]` vazio** no `.cernerc` (todos os campos zero). Um usuário seguindo a doc do byo-key adicionaria `[llm] mode="byo-key"...` e teria **tabela TOML duplicada → erro de parse**. Corrigido: `SaveConfig` omite o `[llm]` quando vazio (wire-struct com ponteiro + `omitempty`); read sites e `NewProvider(valor)` inalterados. TDD: `internal/project/config_omitllm_test.go` (+2 testes). Commit `fix(project): omit empty [llm] block from .cernerc`.

## Decisão de engenharia (ver spec)

HTTP cru via `net/http` em vez do `anthropic-sdk-go` — filosofia de dependências mínimas do cerne; o `Provider` é um único `Complete()` sem tools/streaming/thinking, e a árvore transitiva do SDK não se justifica. Formato de fio conforme a referência oficial da Claude API.

## Verificação

- **Testes Go** (httptest, sem rede): provider (headers/modelo/parse/erros/chave-faltando/provider-não-suportado), `compile --force` byo-key (página escrita+commitada, sem `.pending`), `ask` byo-key (resposta impressa). Modo delegação inalterado.
- **Smoke do binário real** contra um servidor fake da Messages API: `init` (sem `[llm]` vazio) → `[llm] mode=byo-key` → `compile --force` escreveu `modules/core.md` sintetizada com proveniência (`sources`/`compiled_from`/`input_hash`) e commitou; `ask` imprimiu a resposta. O fake validou `x-api-key`, `anthropic-version: 2023-06-01`, `model=claude-opus-4-8` e a ausência de `temperature`/`thinking`.

## Inventário

- **Código:** `internal/project/config.go` (+5 campos, `WithDefaults`, SaveConfig omitempty), `internal/llm/http.go` (provider), `internal/cli/{compilecmd,askcmd}.go` (caminho síncrono).
- **Testes:** `internal/project/{llmconfig_test,config_omitllm_test}.go`, `internal/llm/http_test.go`, `internal/cli/{compilebyokey_test,askbyokey_test}.go` (+15; 342→357).
- **Commits:** `38a1f20`, `994b232`, `9108bc0`, `f4217c8`, fix do `[llm]` vazio, + docs deste case.

## Fora de escopo / follow-ups

- Streaming, thinking, tools, prompt caching (um `Complete` simples basta).
- Outros providers (OpenAI-compatível) — interface pronta.
- UI para compile/ask (segue follow-up do 0003).
- Release pipeline (próximo item da fila pós-v1).
