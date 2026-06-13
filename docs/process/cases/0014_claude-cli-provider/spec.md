# Case 0014 — Provider `claude-cli` (assinatura) + `cerne spec draft`

tags: #spec #cerne #llm #provider #claude-code #spec-authoring

**Status:** Done — implementado e verificado (binário + smoke real na assinatura) (2026-06-13); ver [result.md](result.md)
**Tipo:** Terceira impl do `Provider` (shell ao Claude Code) + comando de autoria de spec.
**Origem:** Pedido do usuário: usar o **plano de IA que já tem (assinatura Claude Code)** em vez de chave de API, e tornar simples o passo "criei a feature → agora a spec". O modo `delegation` (case 0003) já usa a assinatura quando o cerne roda **dentro** do Claude Code; falta o caminho **standalone** na assinatura, e um comando que rascunhe a spec.

---

## Problema

Hoje há dois modos LLM: `delegation` (o agente hospedeiro sintetiza — usa a assinatura, mas só quando o cerne roda dentro de um agente) e `byo-key` (case 0009 — chama a API da Anthropic com chave própria, **cobrança por token**). Quem tem assinatura Claude Code Pro/Max e roda o cerne **standalone** (fora de um agente) não tem como usar o plano que já paga — só a API key. E não existe comando que rascunhe a spec de uma feature: `feature new` só cria um stub vazio.

---

## Objetivo e critérios de sucesso

1. **Provider `claude-cli`**: novo `[llm] mode = "claude-cli"` que faz `Complete` via `claude -p` (Claude Code headless), usando a **assinatura** (sem API key).
2. **`cerne spec draft <id>`**: rascunha a spec de uma feature via o provider configurado (delegação imprime o prompt; byo-key/claude-cli geram e escrevem), materializando o resultado como a spec da feature.
3. Núcleo determinístico e modos `delegation`/`byo-key` inalterados; suíte verde.
4. Provider testável sem `claude` real (seam de runner injetável) + smoke real na assinatura.

---

## Decisões do design (2026-06-13)

| Fork | Decisão | Razão |
|---|---|---|
| **Comando do Claude Code** | `claude -p "<prompt>" --output-format text` | One-shot headless; stdout = só a resposta (confirmado no guia do Claude Code). |
| **Auth = assinatura** | **Não** passar `--bare` (que ignora `CLAUDE_CODE_OAUTH_TOKEN` e exigiria API key); **não** setar `ANTHROPIC_API_KEY` (a key sobrescreve a assinatura). Usa o login Pro/Max (`/login`) ou `CLAUDE_CODE_OAUTH_TOKEN`. | É o ponto: usar a assinatura, não a API. Precedência: API key > assinatura. |
| **Modelo** | claude-cli **não** força `--model` (usa o default do Claude Code) | Evita "modelo indisponível no seu plano"; `[llm] model` segue só para byo-key. |
| **Binário** | `[llm] claude_bin` (default `"claude"`) | Permite override e **fake nos testes** (sem `claude` real). |
| **Testabilidade** | `cliProvider` recebe um **runner injetável** (`func(ctx, name, args) (string, error)`, default = exec real) | Testes injetam fake determinístico; o exec real (que rodaria o Claude Code) não é exercido em unit test. |
| **`spec draft` — destino** | Escreve a spec rascunhada no **backend** `content/features/<id>/spec.md` (frontmatter `cerne_id` carimbado pelo cerne, não pelo modelo) + commit+push | Consistente com `feature new` (que escreve o stub no backend); `cerne_id` determinístico nunca confia no modelo. |
| **`spec draft` em delegação** | Imprime o prompt montado (o agente hospedeiro rascunha) | Mesma semântica de compile/ask em delegação. |
| **Caveat de créditos** | Documentar: a partir de 15/jun/2026, `claude -p` em planos de assinatura consome um **pool separado de créditos do Agent SDK**, não o limite interativo. | Honestidade de uso. |

---

## Desenho

### Provider (`internal/llm`)

- Config: `LLMConfig.ClaudeBin string` (toml `claude_bin`, default `"claude"`).
- `NewProvider(cfg)`: `ResolveMode` → `delegation` ⇒ `ErrNoProviderForDelegation`; `byo-key` ⇒ httpProvider (existente); `claude-cli` ⇒ `cliProvider{bin: cfg.ClaudeBin, run: realRun}` (sem ler chave de env).
- `cliProvider.Complete(ctx, prompt)`: `run(ctx, bin, ["-p", prompt, "--output-format", "text"])`; erro → mensagem acionável ("instale o Claude Code e rode 'claude /login', ou configure [llm] mode='byo-key'"); sucesso → stdout trimado.
- `run` é um campo (`type runner func(ctx, name string, args []string) (string, error)`); default `execRun` (via `exec.CommandContext`, captura stdout, erro com stderr truncado em exit≠0). Testes injetam fake.

### `cerne spec draft <id>` (`internal/cli/speccmd.go`)

- `cerne spec draft <id> [--dir]` (parent `spec`).
- resolveProject (pull) → `store.LoadFeature(id)` (NotFound → erro pt-BR).
- `BuildSpecDraftPrompt(feature, currentContent)` — instrui a escrever uma spec **completa em pt-BR** (Problema, Objetivo e critérios de sucesso, Decisões, Fora de escopo) para a feature (id/título/módulo), partindo do stub atual.
- Modo delegação: imprime o prompt + instrução de onde escrever e `cerne push`.
- Modo byo-key/claude-cli: `prov.Complete(prompt)` → escreve `content/features/<id>/spec.md` = `---\ncerne_id: <id>\n---\n\n` + corpo; commit+push (`cerne: draft spec <id>`); imprime "spec rascunhada e enviada; rode 'cerne pull' para revisar".

---

## Verificação

- **Unit:** `cliProvider.Complete` com runner fake (sucesso + erro); `NewProvider` roteia claude-cli sem exigir chave; `BuildSpecDraftPrompt` inclui id/título/módulo; `cerne spec draft` (byo-key via httptest) escreve a spec + commita.
- **Smoke real (assinatura):** com `claude` no PATH e logado, projeto temp em `mode="claude-cli"`, `cerne spec draft F-0001` → spec real gerada pela assinatura e escrita no backend.

## Fora de escopo

- `feature new --draft-spec` (atalho) — pode vir depois; `spec draft` é o primitivo.
- Draft de plan/result; outros providers.
