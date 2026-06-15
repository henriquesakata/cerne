# Plano — Case 0025 (botão "Gerar com IA" na UI)

TDD estrito, um concern por commit. Suíte tem de ficar verde a cada passo; `go vet` limpo; build da UI ok com mermaid/excalidraw em chunks lazy.

## Etapa 1 — mover o núcleo de geração para `internal/app` (refactor, sem mudança de comportamento)

Server não pode importar `internal/cli` (ciclo via `uicmd.go`). Promovo as funções puras do 0016 para `internal/app`, fonte única.

1. **`internal/app/diagram.go`** (novo): `ErrNotMermaidSequence`, `BuildSequenceDiagramPrompt(f store.Feature, specContent string) string`, `SanitizeMermaid(raw string) (string, error)`, `MermaidArtifactName(name string) string`, e o `extractFencedBlock` interno. Cópia fiel (só exportadas e `sanitizeMermaid`→`SanitizeMermaid`, `mermaidArtifactName`→`MermaidArtifactName`). Importa só `errors`/`fmt`/`strings` + `slug` + `store` — **não** arrasta `llm` para `app`.
2. **`internal/app/diagram_test.go`** (novo): move `TestBuildSequenceDiagramPrompt` + `TestSanitizeMermaid` (de `cli/diagram_test.go`) e `TestMermaidArtifactName` (de `cli/specdiagram_test.go`), adaptados ao pacote `app`.
3. **`internal/cli/diagram.go`**: remove as defs movidas; `runSpecDiagram` chama `app.BuildSequenceDiagramPrompt`/`app.SanitizeMermaid`/`app.MermaidArtifactName`/`app.ErrNotMermaidSequence`. `newSpecDiagramCmd` fica.
4. **`internal/cli/diagram_test.go`**: apagar (ambos os testes migraram). **`internal/cli/specdiagram_test.go`**: remover só `TestMermaidArtifactName`.
5. Verde: `go vet ./... && go test ./internal/app/ ./internal/cli/ -count=1`.

## Etapa 2 — endpoint `POST /api/features/{id}/diagram` (TDD)

1. **`internal/server/diagram_test.go`** (novo, RED) — espelha `ask_test.go`/`artifacts_test.go`:
   - byo-key (provider falso `httptest`) → 200 `{generated:true, name:"sequence.mmd", mermaid:"sequenceDiagram…"}`; artefato salvo em `content/artifacts/<id>/sequence.mmd`.
   - delegação → 200 `{generated:false, needs_provider:true}`; **nenhum** artefato escrito.
   - feature inexistente → 404.
   - saída não-sequence → 422.
2. **`internal/server/server.go`**: registra a rota e adiciona `handleGenerateDiagram(w, r, repoDir, projDir, id)` (fluxo no spec.md, §Desenho/Backend; espelha `handleAsk`). DTOs `generateDiagramRequest{Name}` / `generateDiagramResponse{Generated,NeedsProvider,Name,Mermaid}`.
3. Verde: `go vet ./... && go test ./internal/server/ -count=1`.

## Etapa 3 — UI (botão + wiring)

1. **`ui/src/api.ts`**: `generateDiagram(featureId, name?) → {generated, needs_provider?, name?, mermaid?}` (POST). (Reusa `fetchLLMMode`.)
2. **`ui/src/FeaturePage.tsx`**:
   - `FeaturePage` busca o modo (`fetchLLMMode`) e passa para `DiagramsSection`.
   - `DiagramsSection`: botão **"Gerar com IA"** (ao lado de "Novo diagrama"); desabilitado em `delegation` com dica. Ao clicar: estado `generating`; `generateDiagram(id)`; em `generated:true` reusa `editAsExcalidraw(name, mermaid)` (converte→abre `DiagramPanel` preenchido) + `load()`; `needs_provider`/erro → notice.
3. Build: `cd ui && npm run build` (entry não cresce; mermaid/excalidraw lazy). Embed + suíte: `go build ./... && go vet ./... && go test ./... -count=1`.

## Etapa 4 — fechamento

1. **`result.md`** com números da suíte, inventário e decisões confirmadas.
2. Registrar no guardião: `cerne import` + `cerne pull`; commitar `docs.lock`. (Ação no backend remoto — confirmar antes.)

## Riscos / notas

- **Colisão de número de caso** entre worktrees paralelos: renumerar na entrega (fetch+rebase) se 0025 já existir no main.
- `parseMermaidToExcalidraw` pode falhar em diagramas exóticos; o erro já cai no `convertError` (notice), herdado do 0016.
- Multi-projeto (`serve`) fora de escopo: seu mux não expõe artifacts/geração.
