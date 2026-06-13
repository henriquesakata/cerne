# Resultado — Case 0016 (`cerne spec diagram` + render/edição Mermaid na UI)

**Status:** Done (2026-06-13). Suíte: **425 testes** verdes, `go vet` limpo, build da UI ok (mermaid em chunk lazy). Smoke real na assinatura ✅. Pacote completo entregue: gerar (assinatura) + renderizar (UI) + editar (Excalidraw).

## O que foi entregue

Geração de **diagrama de sequência em Mermaid** via a assinatura Claude Code, guardado como artifact, renderizado na página da feature e convertível para Excalidraw editável — fechando a visão do case 0001 (revisar a spec com diagrama antes de aprovar) com o provider `claude-cli` (case 0014).

### Frente A — backend (Go)

- **Prompt + sanitizer** (`internal/cli/diagram.go`): `BuildSequenceDiagramPrompt(f, specContent)` (pt-BR, inclui id/título/módulo, usa a spec como contexto, pede **só** o código Mermaid `sequenceDiagram` sem cercas). `sanitizeMermaid(raw) (string, error)` remove cercas ```` ```mermaid ```` / ```` ``` ````, faz trim e valida início `sequenceDiagram` — sentinela `ErrNotMermaidSequence`. Commit `e07562e`.
- **`cerne spec diagram <id> [--name]`** (sob `spec`, default `sequence` → `sequence.mmd`): resolveProject; LoadFeature; lê a spec vigente como contexto; delegação imprime o prompt + onde escrever + `cerne push`; byo-key/claude-cli → `prov.Complete` → `sanitizeMermaid` → `app.SaveArtifact(repoDir, projDir, id, "<name>.mmd", bytes)` (commit+push). Commit `551fc75`.
- **Server aceita/serve `.mmd`** (`internal/server/artifacts.go`): GET serve `.mmd` como `text/plain; charset=utf-8` e `.excalidraw` como `application/json`; PUT aceita `.excalidraw` (json.Valid) **ou** `.mmd` (texto não-vazio), senão 400. Commit `fd0b7c5`.

### Frente B — UI (render + conversão)

- **Deps lazy**: `mermaid` (render) + `@excalidraw/mermaid-to-excalidraw` (conversão) — ambos fora do bundle de entrada (chunks `mermaid.core-*.js`, `sequenceDiagram-*.js` etc. só baixam ao abrir um `.mmd`).
- **`api.ts`**: `fetchArtifactText(featureId, name)` devolve o texto do artifact (`.mmd`).
- **`MermaidView.tsx`** (lazy): busca o texto, `mermaid.render` (id único por instância), mostra o SVG; botão **"Editar como Excalidraw"** → `onEdit(source)`; estados loading/erro (erro mostra a mensagem + a fonte).
- **`FeaturePage.tsx`**: `DiagramsSection` ramifica por extensão — `.mmd` → `MermaidView`; `.excalidraw` → `DiagramPanel` (fluxo atual). `convertMermaidToScene(mermaidText)` usa `parseMermaidToExcalidraw` + `convertToExcalidrawElements` e abre o `DiagramPanel` com cena inicial, salvando um `<base>.excalidraw` **irmão** (a `.mmd` é a fonte gerada; editar bifurca). Union `OpenDiagram` (`mermaid` | `excalidraw` com `isNew`/`initialScene`), `excalidrawSiblingName(mmdName)`, tags de tipo (mermaid/excalidraw) na lista. Commit `83c9b1b`.

## Decisões (confirmadas na implementação)

| Fork | Decisão | Razão |
|---|---|---|
| Formato do LLM | **Mermaid `sequenceDiagram`** (não Excalidraw JSON) | LLM é confiável em Mermaid; JSON cru (coords/ids) é frágil. |
| Editável | LLM gera `.mmd`; UI converte para Excalidraw, salvando um `.excalidraw` irmão | Confiabilidade do LLM + edição visual (visão do 0001). |
| Comando | `cerne spec diagram <id> [--name]` sob `spec`, default `sequence` | Coerente com `spec draft`. |
| Server | GET por extensão; PUT valida `.excalidraw` (JSON) **ou** `.mmd` (texto) | Generaliza o handler antes `.excalidraw`-only. |

## Verificação

| Item | Como | Resultado |
|---|---|---|
| `sanitizeMermaid` | unit (com/sem fence, prosa, não-sequence) | extrai/valida; `ErrNotMermaidSequence` quando não começa com `sequenceDiagram` |
| `BuildSequenceDiagramPrompt` | unit | inclui id/título/módulo; pede só Mermaid |
| `cerne spec diagram` byo-key | CLI test (httptest) | `<name>.mmd` salvo + commit + lock |
| server GET/PUT `.mmd` | server test | GET `text/plain`; PUT aceita `.mmd`/`.excalidraw`, rejeita desconhecido (400) |
| **smoke real (assinatura)** | `mode="claude-cli"`, `cerne spec diagram` | **Mermaid `sequenceDiagram` real gerado e salvo** (OAuth flow), `ANTHROPIC_API_KEY` vazia |
| UI build | `npm run build` | `tsc` limpo; mermaid/excalidraw em chunks lazy (entry `index-*.js` ~53 kB) |
| Go embute dist | `go build ./... && go test ./...` | build ok; 425 testes |

## Coordenação multi-agente

Frente B foi desenvolvida em paralelo a uma mudança concorrente de UI do outro agente (`6e9dc0b` — render de markdown com GFM via componente `./Markdown`). Rebase produziu **um** conflito (bloco de imports do `FeaturePage.tsx`): resolvido mantendo `import Markdown from './Markdown'` (corpo usa `<Markdown>`) **e** `import type { ExcalidrawInitialDataState }` (conversão Mermaid→Excalidraw), dropando o `react-markdown` órfão. **Lição do 0014 aplicada:** `npm run build` + `go build && go test` **antes** do push — o dist foi regenerado sobre a fonte mesclada e amendado ao commit da Frente B.

## Inventário

- **Código (Go):** `internal/cli/diagram.go` (novo), `internal/cli/speccmd.go` (registra `spec diagram`), `internal/server/artifacts.go`.
- **Código (UI):** `ui/src/MermaidView.tsx` (novo), `ui/src/FeaturePage.tsx`, `ui/src/api.ts`, `ui/src/DiagramPanel.tsx` (cena inicial), `ui/package.json` (+mermaid, +mermaid-to-excalidraw), `ui/dist` (rebuild).
- **Testes:** `internal/cli/diagram_test.go`, `internal/cli/speccmd_test.go`, `internal/server/artifacts_test.go`.
- **Commits:** `e07562e`, `551fc75`, `fd0b7c5`, `83c9b1b` (+ este result).

## Fora de escopo / follow-ups

- Diagramas além de sequência (flowchart/class) — o comando é específico; o pipeline Mermaid generaliza depois.
- Regenerar o `.mmd` e reconciliar com um `.excalidraw` já bifurcado (hoje a edição é one-way: `.mmd` → `.excalidraw`).
- Render do Mermaid no browser não é automatizável aqui (coberto por build + revisão).
