# Resultado — Case 0014 (provider claude-cli + `cerne spec draft`)

**Status:** Done (2026-06-13). Suíte: 391 testes verdes, `go vet` limpo, **zero** dependência nova. Smoke real na assinatura ✅.

## O que foi entregue

Usar a **assinatura Claude Code** (sem API key) e um comando que rascunha a spec de uma feature.

- **Provider `claude-cli`** (`internal/llm/cli_provider.go`): `[llm] mode = "claude-cli"`. `cliProvider.Complete` faz `claude -p "<prompt>" --output-format text` (argv exato `["-p", prompt, "--output-format", "text"]`, **sem** `--bare`/`--model` — usa o modelo default do Claude Code e a auth da assinatura). Runner injetável (`run` field, default `execRun`) para testes determinísticos. `NewProvider` roteia por modo (delegation→sentinela, byo-key→http, claude-cli→cli) e **não lê chave** no modo claude-cli. Sentinela `ErrClaudeCLI` com mensagem acionável. Config `claude_bin` (default `"claude"`). Commits `a724821`, `c20e739`.
- **`cerne spec draft <id>`** (`internal/cli/speccmd.go`): `BuildSpecDraftPrompt` (pt-BR: Problema/Objetivo/Decisões/Fora de escopo, com id/título/módulo). Delegação imprime o prompt; byo-key/claude-cli geram e escrevem `content/features/<id>/spec.md` com `cerne_id` **carimbado pelo cerne** (modelo nunca emite frontmatter) + commit+push. Commit `76f607d`.

## Como usar a assinatura (resumo)

1. `claude /login` (Pro/Max) — ou `claude setup-token` → `CLAUDE_CODE_OAUTH_TOKEN` (headless).
2. **Não** setar `ANTHROPIC_API_KEY` (key sobrescreve a assinatura).
3. `.cernerc`: `[llm]\nmode = "claude-cli"`.
4. `cerne spec draft <id>` / `cerne compile` / `cerne ask` passam a usar o plano, sem custo por token.

**Caveat:** a partir de 15/jun/2026, `claude -p` em planos de assinatura consome um **pool separado de créditos do Agent SDK** (não o limite interativo).

## Verificação

| Item | Como | Resultado |
|---|---|---|
| argv do provider | unit (runner fake) | `["-p", <prompt>, "--output-format", "text"]`; sem `--bare`/`--model` |
| sem API key | `NewProvider(claude-cli)` com env sem chave | sem `ErrAPIKeyMissing` |
| erro do CLI | runner fake com erro | `ErrClaudeCLI` (errors.Is) |
| `spec draft` byo-key | CLI test via httptest | spec escrita + `cerne_id` + commit + lock |
| **smoke real (assinatura)** | `claude` logado, projeto temp `mode="claude-cli"`, `cerne spec draft F-0001` | **`claude -p` chamado na assinatura → spec real gerada** ("Exportar relatório mensal em PDF": Problema/Objetivo em pt-BR) escrita no backend com frontmatter; `ANTHROPIC_API_KEY` vazia confirmada |

## Resposta ao pedido do usuário

"Usar a assinatura em vez da API key, e tornar simples feature→spec": com `mode="claude-cli"`, `cerne feature new "X"` → `cerne spec draft F-XXXX` rascunha a spec na assinatura → revisar (`cerne pull`) → `cerne feature approve`. Dentro do Claude Code, o modo `delegation` (default) já fazia isso sem config; o claude-cli estende para o cerne **standalone**.

## Inventário

- **Código:** `internal/project/config.go` (claude_bin), `internal/llm/cli_provider.go` + `http.go` (rota), `internal/cli/speccmd.go` + `root.go`.
- **Testes:** `internal/llm/cli_provider_test.go`, `internal/cli/speccmd_test.go`, `internal/project/llmconfig_test.go` (+8; 383→391).
- **Commits:** `a724821`, `c20e739`, `76f607d` + docs.

## Fora de escopo / follow-ups

- `cerne feature new --draft-spec` (atalho de um comando só).
- Draft de plan/result; provider OpenAI-compatível.
- Skill do cerne que guie o agente no fluxo (delegação) ponta a ponta.
