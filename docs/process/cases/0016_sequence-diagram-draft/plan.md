# Plano — Case 0016 (diagrama de sequência via assinatura + UI)

**Goal:** `cerne spec diagram <id>` gera Mermaid sequenceDiagram via o provider (assinatura no claude-cli), guarda como `.mmd`; a UI renderiza e converte para Excalidraw editável.

## Frente A — backend (dispatch 1, TDD)

1. **Sanitizer + prompt** (`internal/cli/speccmd.go` ou novo `diagram.go`): `sanitizeMermaid(raw)` (remove fence ```mermaid/```, trim, erro `ErrNotMermaidSequence` se não começa com `sequenceDiagram`); `BuildSequenceDiagramPrompt(f, spec)` (pt-BR, inclui id/título/módulo, pede só o código Mermaid). Testes. → commit `feat(cli): mermaid sequence-diagram prompt + sanitizer`.
2. **`cerne spec diagram <id> [--name]`**: sob `spec`; delegação imprime prompt; provider modes → Complete → sanitizeMermaid → `app.SaveArtifact(..., "<name>.mmd", bytes)` → mensagem. Teste byo-key (httptest) salva `<name>.mmd` + commit + lock. → commit `feat(cli): cerne spec diagram — AI-draft a Mermaid sequence diagram`.
3. **Server `.mmd`** (`internal/server/artifacts.go` + teste): GET `.mmd`→`text/plain; charset=utf-8`; PUT aceita `.mmd` (texto não-vazio) ou `.excalidraw` (JSON). Testes GET/PUT `.mmd` + rejeição de extensão desconhecida. → commit `feat(server): accept and serve .mmd artifacts`.

Verificar: `go vet` + suíte verdes; **smoke real** `mode=claude-cli` → `cerne spec diagram F-0001` gera Mermaid de verdade. Push (rebuild após rebase!).

## Frente B — UI (dispatch 2, após re-sync)

4. deps `mermaid` + `@excalidraw/mermaid-to-excalidraw`; `api.ts` `fetchArtifactText`; `MermaidView` (lazy) renderiza `.mmd`; `DiagramsSection` ramifica por extensão; botão "Editar como Excalidraw" converte → `DiagramPanel` (cena inicial) salvando `<base>.excalidraw`. Build (`npm ci && npm run build`), conferir chunk lazy do mermaid; commitar `ui/dist`. → commit `feat(ui): render .mmd diagrams (mermaid) + convert to editable Excalidraw`.

Verificar: `go test ./...` (server embute dist novo) verde; `cerne ui` serve; bundle contém mermaid em chunk separado.
