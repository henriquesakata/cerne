# Resultado — Case 0025 (botão "Gerar com IA" na UI: Mermaid → Excalidraw editável)

**Status:** Done (2026-06-15). Suíte: **468 testes** verdes, `go vet` limpo, build da UI ok (entry `index-*.js` segue ~53 kB; mermaid/excalidraw em chunks lazy). Estende o case 0016 (geração CLI) para a web.

## O que foi entregue

Um botão **"Gerar com IA"** na aba Diagramas da página da feature: a IA gera um diagrama de sequência **Mermaid**, ele é salvo como artefato `.mmd` no backend (commit+push), **convertido para Excalidraw** e aberto **já preenchido e editável** — a experiência "a IA preenche o canvas", construída sobre o pipeline confiável do 0016 e a conversão Mermaid→Excalidraw que já existia na UI.

### Frente A — núcleo compartilhado (refactor, sem mudança de comportamento)

- **`internal/app/diagram.go`** (novo): `BuildSequenceDiagramPrompt`, `SanitizeMermaid` (+ `ErrNotMermaidSequence`), `MermaidArtifactName` e o `extractFencedBlock` interno — promovidos de `internal/cli` para `internal/app` (server não pode importar `cli` → ciclo via `uicmd.go`). Funções puras (só `slug`+`store`); `llm` **não** entra em `app`.
- **`internal/cli/diagram.go`**: passa a chamar as versões de `app`; `newSpecDiagramCmd`/`runSpecDiagram` intactos no comportamento.
- Testes movidos para **`internal/app/diagram_test.go`** (`TestBuildSequenceDiagramPrompt`, `TestSanitizeMermaid`, `TestMermaidArtifactName`); `cli/diagram_test.go` removido; `cli/specdiagram_test.go` mantém só os testes de comando (`TestSpecDiagram*`).

### Frente B — endpoint `POST /api/features/{id}/diagram`

- **`internal/server/server.go`** (`handleGenerateDiagram`): espelha `handleAsk` — config por-request (trocar o modo na UI vale na hora, sem restart); `delegation` (ou sem `.cernerc`) → 200 `{generated:false, needs_provider:true}` (não inventa, não escreve); senão `provider.Complete(app.BuildSequenceDiagramPrompt(f, spec))` → `app.SanitizeMermaid` → `app.SaveArtifact`. Erros: feature inexistente → 404; saída não-sequence → 422; provider → 500. Sucesso → 200 `{generated:true, name, mermaid}` (devolve o Mermaid para a UI converter sem segundo fetch).
- DTOs `generateDiagramRequest{Name}` / `generateDiagramResponse{Generated,NeedsProvider,Name,Mermaid}`.

### Frente C — UI

- **`ui/src/api.ts`**: `generateDiagram(featureId, name?) → GenerateDiagramResult` (POST; mapeia `needs_provider`→`generated:false`).
- **`ui/src/FeaturePage.tsx`**: `FeaturePage` lê o modo (`fetchLLMMode`, case 0019) e o passa à `DiagramsSection`. Botão **"Gerar com IA"** (desabilitado em `delegation` com dica via `title`); ao clicar: estado `generating`; `generateDiagram` → `convertMermaidToScene` (reuso) → `onOpen` abre o `DiagramPanel` com `initialScene` (nome `<base>.excalidraw`) + `onRefresh()` para a `.mmd` aparecer na lista. `needs_provider`/erro → `genError` (notice).
- **`ui/src/app.css`**: novo `.diagram-actions` (flex) agrupa "Gerar com IA" + "Novo diagrama"; seletores de botão migrados de `.artifacts > button` para `.diagram-actions button`.

## Decisões (confirmadas na implementação)

| Fork | Decisão | Razão |
|---|---|---|
| Formato | Mermaid `sequenceDiagram` convertido para Excalidraw editável no cliente | Escolha do usuário; reusa 0016 + conversão existente; LLM emitir Excalidraw cru foi rejeitado no 0016 e providers só fazem texto→texto. |
| Núcleo | Promovido para `internal/app` (puro, sem `llm`) | Compartilhado CLI+server; server não importa `cli`. |
| Orquestração | No handler, igual a `handleAsk` | Mantém `app` livre de `llm`; reusa padrão/erros testados. |
| Delegação | Botão desabilitado na UI + guarda no endpoint (`needs_provider`) | Espelha `/api/ask`; nunca inventa. |
| Escopo | Só sequência; só single-project (`cerne ui`) | Mantém escopo; `serve` multi-projeto não expõe artifacts (0022). |

## Verificação

| Item | Como | Resultado |
|---|---|---|
| `SanitizeMermaid`/`BuildSequenceDiagramPrompt`/`MermaidArtifactName` | unit (movidos p/ `app`) | verdes |
| `POST .../diagram` byo-key | server test (provider falso httptest) | 200 `generated:true`, `sequence.mmd` salvo, origin avança |
| `POST .../diagram` delegação | server test | 200 `needs_provider:true`, nada escrito |
| feature inexistente | server test | 404 |
| saída não-sequence | server test | 422, nada escrito |
| CLI `spec diagram` | `specdiagram_test.go` | verde após refactor |
| UI | `tsc` + `vite build` | limpo; entry ~53 kB; mermaid/excalidraw lazy |
| Suíte + embed | `go build ./... && go vet ./... && go test ./...` | 468 testes, vet limpo |

**Não automatizável aqui:** o render no browser e o clique no botão (convenção do 0011/0016). Verificação viva: reinstalar o binário (`go install ./cmd/cerne`), recarregar a UI, abrir uma feature, clicar "Gerar com IA" (no modo assinatura/chave própria).

## Inventário

- **Go (novo):** `internal/app/diagram.go`, `internal/app/diagram_test.go`, `internal/server/diagram_test.go`.
- **Go (alterado):** `internal/cli/diagram.go`, `internal/cli/specdiagram_test.go`, `internal/server/server.go`. **Removido:** `internal/cli/diagram_test.go`.
- **UI:** `ui/src/api.ts`, `ui/src/FeaturePage.tsx`, `ui/src/app.css`, `ui/dist` (rebuild embarcado).

## Fora de escopo / follow-ups

- Diagramas além de sequência (flowchart/class) — como no 0016.
- Geração no mux multi-projeto (`cerne serve`, 0022) — exige expor artifacts/geração lá.
- Regenerar o `.mmd` reconciliando com um `.excalidraw` já bifurcado (edição one-way, herdado do 0016).
- Campo para nomear o diagrama antes de gerar (hoje default `sequence`).
