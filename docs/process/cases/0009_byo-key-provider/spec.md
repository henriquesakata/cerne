# Case 0009 — Provider LLM BYO-key (chave própria)

tags: #spec #cerne #llm #compile #ask #provider

**Status:** Done — implementado e verificado (2026-06-13); ver [result.md](result.md)
**Tipo:** Nova implementação do `Provider` (HTTP, Anthropic Messages API) + ligação do caminho síncrono em `compile`/`ask`.
**Origem:** Dívida do [case 0003](../0003_llm-compile-ask/spec.md) (e reiterada no 0005): o caminho síncrono `Provider.Complete` foi removido do runtime — só a delegação ao agente hospedeiro existe, e o `Fake` só roda em teste. A interface BYO-key estava "documentada mas inerte". Este case a torna real.

---

## Problema

`cerne compile`/`ask` (case 0003) só funcionam em **modo delegação** (o agente hospedeiro faz a síntese, em duas fases). Hoje os comandos **rejeitam** explicitamente qualquer outro modo (`internal/cli/compilecmd.go`, `askcmd.go`: `modo LLM %q ainda não suportado`). Quem quer rodar o cerne **standalone** (sem um agente Claude Code/Cursor já em execução) não tem como gerar a wiki nem responder perguntas — falta um provider que chame um LLM diretamente, com chave própria.

---

## Objetivo e critérios de sucesso

Modo `byo-key`: `compile`/`ask` chamam um LLM via HTTP, com a chave fornecida pelo usuário, **em uma fase** (síncrono). O núcleo determinístico segue intacto e o default segue sendo delegação.

**Critério de aceite:**
1. Núcleo 100% funcional **sem** `[llm]` ou com `mode="delegation"` — comportamento do v1 inalterado; suíte existente verde.
2. Com `mode="byo-key"` e chave no env: `cerne compile` sintetiza cada página suja **em uma fase** (chama o provider, escreve `content/wiki/<rel>.md` com frontmatter de proveniência, um commit+push) — sem `.pending`, sem `--ingest`.
3. Com `mode="byo-key"`: `cerne ask` chama o provider com o prompt montado e **imprime a resposta** (em vez do prompt).
4. Provider HTTP testado contra `httptest.Server` (sem rede): headers corretos (`x-api-key`, `anthropic-version`), modelo, mensagens; parse da resposta; erro em não-200; erro claro quando a chave falta.
5. Segredo **nunca** mora no `.cernerc` — só no env nomeado por `key_env`.

---

## Decisões do design (2026-06-13)

| Fork | Decisão | Razão |
|---|---|---|
| **Cliente HTTP** | **`net/http` cru** contra a Messages API (não o `anthropic-sdk-go`) | Filosofia de dependências mínimas do cerne; `Provider` é um único `Complete()` sem tools/streaming/thinking — a árvore transitiva do SDK não se justifica. Formato de fio documentado (cURL ref da Claude API). |
| **Endpoint/wire** | `POST {base_url}/v1/messages`, headers `x-api-key` + `anthropic-version: 2023-06-01` + `content-type`; body `{model, max_tokens, messages:[{role:"user",content:prompt}]}` | Conforme a referência oficial. **Sem** `temperature`/`top_p`/`top_k`/`thinking` (removidos/400 no Opus 4.8). |
| **Modelo default** | `claude-opus-4-8` (configurável via `[llm] model`) | Diretriz: default no modelo mais capaz; usuário sobrescreve. |
| **Chave** | Lida do env nomeado por `[llm] key_env` (default `CERNE_LLM_API_KEY`) | Segredo nunca no `.cernerc`. |
| **Provider plugável** | Interface `Provider` mantida; `anthropic` é a única impl concreta; `base_url` configurável | `base_url` permite proxy e **httptest** nos testes; outro provider entra atrás da mesma interface depois. |
| **Caminho síncrono** | `compile`/`ask` em **uma fase** quando `mode!=delegation` | É o caminho que faltava; reusa montagem de prompt e stamping de proveniência já existentes. |

---

## Desenho

### Config (`internal/project/config.go`, bloco `[llm]`)

Estende `LLMConfig` (hoje só `Mode`):

```toml
[llm]
mode      = "byo-key"            # "delegation" (default) | "byo-key"
provider  = "anthropic"          # default "anthropic"
model     = "claude-opus-4-8"    # default "claude-opus-4-8"
key_env   = "CERNE_LLM_API_KEY"  # nome do env com a chave (default)
base_url  = "https://api.anthropic.com"  # default; override p/ proxy/testes
max_tokens = 8000                # default
```

Segredo nunca aqui — só `key_env` (o **nome** do env).

### `internal/llm` — provider HTTP

```go
// NewProvider builds the configured Provider. For byo-key it reads the API key
// from the env var named by cfg.KeyEnv (error if unset) and returns an HTTP
// provider for cfg.Provider. Delegation mode needs no Provider.
func NewProvider(cfg project.LLMConfig) (Provider, error)
```

- `httpProvider` implementa `Complete(ctx, prompt) (string, error)`: monta o JSON, faz `POST {base_url}/v1/messages` com os headers, lê `content[0].text` da resposta; erro com status+corpo em não-200.
- `Fake` (existente) segue para testes determinísticos.
- Defaults aplicados em `NewProvider`/no parse do config (`provider=anthropic`, `model=claude-opus-4-8`, `key_env=CERNE_LLM_API_KEY`, `base_url=https://api.anthropic.com`, `max_tokens=8000`).

### `compile` síncrono (`internal/cli/compilecmd.go` + `internal/compile`)

Quando `ResolveMode != delegation`: `NewProvider(cfg.LLM)`; para cada página suja, `Complete(BuildPagePrompt(page))`, escreve `content/wiki/<rel>.md` com o frontmatter de proveniência (`sources`/`compiled_from`/`input_hash`) — reusando o stamping do `--ingest` — e ao fim **um** commit+push + lock. Sem `.pending`.

### `ask` síncrono (`internal/cli/askcmd.go`)

Quando `ResolveMode != delegation`: `NewProvider(cfg.LLM)`; `Complete(BuildAskPrompt(query, snippets))`; imprime a resposta no stdout.

---

## Verificação

- Testes Go com `httptest.Server` via `base_url`: provider (headers/modelo/parse/erros/chave-faltando), `compile --force` byo-key (página escrita+commitada via fake/httptest), `ask` byo-key (resposta impressa).
- `go vet` + suíte completa verdes; modo delegação inalterado.

---

## Fora de escopo

- Streaming, thinking, tools, prompt caching (um `Complete` simples basta).
- Outros providers (OpenAI-compatível) — interface pronta, impl futura.
- UI (compile/ask via UI) — segue follow-up do 0003.
