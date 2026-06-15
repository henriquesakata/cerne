# Case 0026 — geração de diagrama ancorada no código e estrutura do projeto

tags: #spec #cerne #llm #diagram #mermaid #prompt

**Status:** Draft (2026-06-15)
**Tipo:** Enriquecimento do prompt de geração (`BuildSequenceDiagramPrompt`) + helper de estrutura do projeto. Aplica-se à CLI (`cerne spec diagram`) e ao endpoint do case 0025.
**Origem:** Pedido do usuário — "para fazer o diagrama de sequência, analisar o código e a estrutura do projeto para ser mais efetivo na geração". Hoje o prompt só usa id/título/módulo + a spec em prosa; nada do código real.

---

## Problema

`BuildSequenceDiagramPrompt(f, specContent)` (case 0016) monta o prompt apenas com id/título/módulo e a spec. O modelo desenha "no escuro": participantes e mensagens saem da prosa, não da implementação. O resultado é genérico e frequentemente desalinhado com os componentes reais (pacotes, handlers, tipos, chamadas).

Insight sobre o provider (verificado em `internal/llm/cli_provider.go`):
- **`claude-cli`** roda `claude -p` (agente Claude Code headless) no diretório do projeto — **tem ferramentas e pode ler o código-fonte**.
- **`delegation`** imprime o prompt para o agente hospedeiro, que também tem ferramentas.
- **`byo-key`** é `Complete()` puro (HTTP) — **sem ferramentas**; só enxerga o que está no prompt.

Logo, a melhoria precisa servir aos três: dar a **estrutura no prompt** (essencial para byo-key) e **instruir a ler os fontes** (aproveitado por claude-cli/delegation).

---

## Objetivo e critérios de sucesso

1. `app.ProjectStructure(projDir)` devolve uma **árvore compacta** dos arquivos do projeto (a partir de `projDir` = onde o `.cernerc` vive = a raiz do repo onde o cerne roda), pulando diretórios pesados/gerados/ignorados e com **teto** de entradas.
2. `BuildSequenceDiagramPrompt(f, specContent, projectStructure)` inclui a estrutura (quando houver) e instruções de **ancoragem no código**: participantes = componentes reais; mensagens = chamadas reais; e, *quando houver acesso ao repositório*, **examinar os fontes relevantes** antes de desenhar.
3. CLI (`runSpecDiagram`) e server (`handleGenerateDiagram`) calculam a estrutura do seu `projDir` e a passam ao prompt. Best-effort: falha ao ler estrutura **não** aborta a geração (segue sem ela).
4. Suíte + `go vet` verdes; build da UI inalterado (mudança é só backend/prompt).

---

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Fonte do código** | `projDir` (diretório onde o cerne roda; raiz do repo com `.cernerc`) | É o único código disponível ao gerador. Correto no caso dogfooding/mono-repo (features descrevem o repo onde o cerne roda). |
| **Estrutura vs conteúdo** | **Estrutura** (lista de caminhos) no prompt + instrução para o agente **ler** os fontes | Estrutura é barata e serve byo-key; ler-os-arquivos aproveita as ferramentas de claude-cli/delegation. Embutir conteúdo de arquivos (RAG p/ byo-key) fica como follow-up. |
| **Coleta** | Walk determinístico de `projDir`, pulando `.git`, `node_modules`, `dist`, `vendor`, `build`, `target`, `.docs` e dot-dirs; ordenado; teto de entradas (trunca com aviso) | Dependência-zero, funciona mesmo fora de git; o skip-set elimina o `ui/dist` (200+ arquivos) e afins. |
| **Assinatura** | `BuildSequenceDiagramPrompt(f, spec, structure string)` | Um parâmetro a mais; ambos os call sites já têm `projDir`. |
| **Best-effort** | Erro/vazio em `ProjectStructure` → prompt sem a seção de estrutura | Ancoragem é melhoria, não pré-requisito; nunca quebrar a geração. |

---

## Desenho

### `internal/app/diagram.go`
- **`ProjectStructure(projDir string) (string, error)`**: `filepath.WalkDir(projDir)`, pulando o skip-set (e dot-dirs); coleta caminhos relativos (com `/`); ordena; se exceder o teto (ex.: 400), trunca e anexa `… (estrutura truncada)`. Junta por `\n`. Vazio quando não há nada.
- **`BuildSequenceDiagramPrompt(f, specContent, projectStructure string)`**: mantém Feature + Spec; quando `projectStructure != ""`, acrescenta a seção "Estrutura do projeto (arquivos versionados; use para identificar os componentes reais)"; acrescenta o bloco de ancoragem (participantes/mensagens reais; examinar os fontes relevantes se houver acesso ao repositório). Fecho inalterado ("apenas Mermaid, sem cercas").

### Call sites
- **`internal/cli/diagram.go`** (`runSpecDiagram`): `structure, _ := app.ProjectStructure(ctx.projDir)`; `prompt := app.BuildSequenceDiagramPrompt(f, spec, structure)`.
- **`internal/server/server.go`** (`handleGenerateDiagram`): `structure, _ := app.ProjectStructure(projDir)`; idem. (Delegação não usa o prompt no server; o caminho byo-key/claude-cli sim.)

---

## Verificação

- **Unit `ProjectStructure`**: temp dir com `cmd/x.go`, `internal/app/y.go`, `node_modules/junk.js`, `dist/bundle.js`, `.docs/CONTEXT.md` → resultado inclui os fontes, **exclui** `node_modules`/`dist`/`.docs`; ordenado; teste de teto/truncamento.
- **Unit `BuildSequenceDiagramPrompt`**: com estrutura → inclui os caminhos + a instrução de ancoragem; com estrutura vazia → sem a seção (e ainda pede Mermaid). Mantém id/título/módulo.
- **CLI/server**: testes existentes seguem verdes com a nova assinatura (estrutura do `projDir` de teste é pequena/vazia — não quebra as asserções).
- `go vet ./... && go test ./... -count=1` verdes.

---

## Fora de escopo / follow-ups

- **Embutir conteúdo de arquivos relevantes** no prompt (RAG sobre o código) para fortalecer o `byo-key` — exige seleção por relevância (busca por termos da feature). Próximo passo natural.
- **Código em outro repo** que não o `projDir` (ex.: feature do Studio com o cerne rodando noutro lugar) — exigiria um ponteiro de localização de código por projeto/feature. Fora de escopo; a ancoragem vale quando o cerne roda no repo que as features descrevem.
- Filtrar a estrutura por módulo/feature (hoje é o projeto todo, com teto).
