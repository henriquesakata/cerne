# Plano — Case 0026 (diagrama ancorado no código)

TDD, um concern. Backend-only (sem mudança de UI/dist).

## Etapa 1 — núcleo + testes (TDD)

1. **Testes (RED)** em `internal/app/diagram_test.go`: `TestBuildSequenceDiagramPrompt` passa a 3 args (verifica a estrutura embutida + "componentes reais"); `TestBuildSequenceDiagramPromptNoStructure` (estrutura vazia → sem seção); `TestProjectStructure` (walk pula `node_modules`/`dist`/`.docs`/dot-dirs, paths com `/`); `TestFormatStructure` (ordena, teto, marcador de truncamento).
2. **`internal/app/diagram.go`**: `ProjectStructure(projDir)` (WalkDir + skip-set + `formatStructure`), `formatStructure(paths, max)`, `maxStructureEntries=400`, `skipDirs`. `BuildSequenceDiagramPrompt(f, spec, structure)` inclui a seção (quando não-vazia) + bloco de ancoragem.
3. Verde: `go test ./internal/app/ -run "Diagram|Structure|Mermaid"`.

## Etapa 2 — call sites

1. `internal/cli/diagram.go` (`runSpecDiagram`) e `internal/server/server.go` (`handleGenerateDiagram`): `structure, _ := app.ProjectStructure(<projDir>)` (best-effort) → `BuildSequenceDiagramPrompt(f, spec, structure)`.
2. Verde: `go build ./... && go vet ./... && go test ./... -count=1`.

## Etapa 3 — verificação viva + fechamento

1. Smoke em `delegation` (só imprime o prompt): `cerne spec diagram F-0001` mostra a estrutura real + ancoragem, sem vazar `ui/dist`/`node_modules`.
2. `result.md`; índice; commits; registrar no backend (`cerne import` + `pull`, `docs.lock`).
