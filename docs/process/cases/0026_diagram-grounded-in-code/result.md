# Resultado — Case 0026 (diagrama ancorado no código e estrutura do projeto)

**Status:** Done (2026-06-15). Suíte: **471 testes** verdes, `go vet` limpo. Backend-only (sem mudança de UI). Estende o prompt do case 0016/0025 para ancorar a geração no código real.

## O que foi entregue

A geração de diagrama de sequência deixou de desenhar "no escuro": o prompt agora inclui a **estrutura do projeto** (arquivos versionados) e instrui o modelo a **ancorar participantes/mensagens em componentes reais** e, quando tem acesso ao repositório (`claude-cli`/`delegation`), a **ler os arquivos-fonte relevantes** antes de desenhar.

- **`internal/app/diagram.go`**:
  - `ProjectStructure(projDir)` — `WalkDir` a partir do `projDir` (raiz do repo onde o cerne roda), pulando `node_modules`/`dist`/`vendor`/`build`/`target`/`.docs` e dot-dirs/dotfiles; caminhos relativos com `/`, ordenados, com teto de **400** (`formatStructure` trunca com aviso).
  - `BuildSequenceDiagramPrompt(f, spec, structure)` — embute a estrutura (quando não-vazia) + bloco de ancoragem; fecho "apenas Mermaid" inalterado.
- **Call sites** (`internal/cli/diagram.go`, `internal/server/server.go`): `structure, _ := app.ProjectStructure(<projDir>)` (best-effort — falha/vazio só omite a seção, nunca aborta) → passam ao prompt.

## Decisão de design por modo de IA (confirmada)

| Modo | Tem ferramentas? | Como a melhoria ajuda |
|---|---|---|
| `claude-cli` (assinatura) | Sim (`claude -p` roda no `projDir`) | Estrutura aponta os caminhos; o agente **lê os fontes** e ancora o fluxo na implementação. |
| `delegation` | Sim (agente hospedeiro) | Idem — o prompt impresso traz a estrutura + a instrução de ler os fontes. |
| `byo-key` | Não (`Complete()` puro) | Ganha pelo menos a **estrutura** embutida (sem ler arquivos). Embutir conteúdo é follow-up. |

## Verificação

| Item | Como | Resultado |
|---|---|---|
| `ProjectStructure` | unit (walk + skip-set + forward-slash) | inclui fontes, exclui `node_modules`/`dist`/`.docs`/`.git` |
| `formatStructure` | unit (teto=2 sobre 4 paths) | ordena, corta em 2, marca "truncada"; sem marca abaixo do teto |
| `BuildSequenceDiagramPrompt` | unit (com/sem estrutura) | embute paths + "componentes reais"; omite a seção quando vazia |
| **smoke viva** | `cerne spec diagram F-0001` em `delegation` (binário recém-buildado) | prompt traz a seção "Estrutura do projeto", a ancoragem e **283 caminhos reais**; **0** vazamento de `ui/dist`/`node_modules` |
| Suíte | `go build ./... && go vet ./... && go test ./...` | 471 testes, vet limpo |

## Inventário

- **Go (alterado):** `internal/app/diagram.go` (+`ProjectStructure`/`formatStructure`, nova assinatura do prompt), `internal/app/diagram_test.go`, `internal/cli/diagram.go`, `internal/server/server.go`.
- Sem mudança de UI/`dist`.

## Fora de escopo / follow-ups

- **Embutir conteúdo de arquivos relevantes** (RAG sobre o código) para fortalecer o `byo-key`, que não lê arquivos — próximo passo natural; exige seleção por relevância (termos da feature).
- **Código em outro repo** que não o `projDir` (ex.: feature do Studio com o cerne rodando noutro lugar) — exigiria ponteiro de localização de código por projeto/feature.
- Filtrar a estrutura por módulo/feature (hoje é o projeto todo, com teto de 400).
