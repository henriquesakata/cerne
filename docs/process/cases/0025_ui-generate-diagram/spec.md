# Case 0025 — botão "Gerar com IA" na UI: diagrama de sequência (Mermaid → Excalidraw editável)

tags: #spec #cerne #llm #diagram #mermaid #excalidraw #ui

**Status:** Draft (2026-06-15)
**Tipo:** Endpoint de geração no server + botão na página da feature (UI). Estende o case 0016 (geração CLI) para a web.
**Origem:** Pedido do usuário — ao abrir a aba Diagramas de uma feature, "Novo diagrama" só abre um Excalidraw em branco; a IA deveria **gerar o diagrama automaticamente**, como na experiência de um "Excalidraw MCP app" (a IA preenche o canvas e você edita).

---

## Problema

O case 0016 entregou a geração de diagrama de sequência por IA, mas **só pela CLI** (`cerne spec diagram <id>`) e **só nos modos `byo-key`/`claude-cli`** (no `delegation`, default, apenas imprime o prompt). Na UI não há gatilho de geração: a `DiagramsSection` (FeaturePage) só tem "Novo diagrama" (canvas Excalidraw vazio, manual) e a abertura de artefatos existentes (`.mmd` renderiza no `MermaidView`; `.excalidraw` abre no `DiagramPanel`). Resultado: quem usa a web não consegue pedir um diagrama à IA — vê um canvas em branco e pensa que deveria ter sido preenchido.

---

## Objetivo e critérios de sucesso

1. Botão **"Gerar com IA"** na `DiagramsSection` da página da feature. Ao clicar (nos modos `byo-key`/`claude-cli`): a IA gera um diagrama de sequência **Mermaid**, ele é salvo como artefato `content/artifacts/<id>/<nome>.mmd` (commit+push), **convertido para Excalidraw** e aberto **já preenchido e editável** no `DiagramPanel` (salvando um `<nome>.excalidraw` irmão ao salvar). É a experiência "a IA preenche o canvas".
2. Novo endpoint `POST /api/features/{id}/diagram` que executa o pipeline do 0016 no server, **espelhando `handleAsk`**: config por-request; em `delegation` devolve `needs_provider` (não inventa); senão `provider.Complete` → sanitiza → salva → devolve o texto Mermaid.
3. Núcleo de geração (prompt + sanitizer + nome do artefato) **compartilhado** entre CLI e server: movido de `internal/cli` para `internal/app` (server não pode importar `cli` — ciclo). A CLI passa a chamar as versões de `app`; comportamento idêntico.
4. No modo `delegation`, o botão fica **desabilitado** na UI (modo lido via `/api/config/llm`) com dica para trocar o modo; o endpoint também guarda (defense-in-depth).
5. Suíte + `go vet` verdes; build da UI ok (mermaid/excalidraw seguem em chunks lazy; o bundle de entrada não cresce).

---

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Formato gerado** | **Mermaid `sequenceDiagram`** convertido para Excalidraw editável no cliente (reusa `convertMermaidToScene`) | Escolha do usuário (Mermaid→Excalidraw). Reaproveita o pipeline confiável do 0016 e a conversão que já existe na UI. O LLM emitir Excalidraw cru foi rejeitado no 0016 (coordenadas/ids frágeis) e os providers do cerne só fazem texto→texto. |
| **Onde mora o núcleo** | Mover `BuildSequenceDiagramPrompt` / `sanitizeMermaid` / `mermaidArtifactName` / `ErrNotMermaidSequence` de `internal/cli/diagram.go` para `internal/app/diagram.go` (exportados) | `internal/app` é o "shared transition/artifact core (CLI + server call it)"; server não pode importar `cli`. Funções puras (só `store`+`slug`), sem arrastar `llm` para `app`. |
| **Orquestração do provider** | Fica **no handler**, igual a `handleAsk` (não em `app`): carrega config por-request, checa `delegation`, cria `provider`, `Complete`, `app.SanitizeMermaid`, `app.SaveArtifact` | Mantém `app` livre de `llm`; reusa o padrão e os erros já testados do 0016/0017. |
| **Endpoint** | `POST /api/features/{id}/diagram`, corpo `{ "name"?: string }` (default `sequence`) | Verbo de ação sob a feature, como `/transition`. Coerente com `cerne spec diagram <id>`. |
| **Resposta** | Sucesso: `{ generated: true, name: "<nome>.mmd", mermaid: "<texto>" }`. Delegação: `{ generated: false, needs_provider: true }` | Devolver o Mermaid deixa a UI converter+abrir sem segundo fetch. `needs_provider` espelha `/api/ask` (200, não erro) → tratamento uniforme na UI. |
| **Persistência** | Salva o `.mmd` (commit+push via `app.SaveArtifact`); editar bifurca um `.excalidraw` irmão (semântica do 0016) | O `.mmd` é a fonte gerada e indexável; a edição visual é one-way `.mmd`→`.excalidraw`. |
| **Tipo de diagrama** | Só **sequência** (como `spec diagram` / 0016) | Mantém escopo; o pipeline Mermaid generaliza depois. |
| **Server** | Só o single-project (`cerne ui` → `server.New`). O mux multi-projeto (`serve`, case 0022) não expõe artifacts/geração | Coerente com 0022 (multi-projeto é um subconjunto: sem artifacts/transition/docs). Fica como follow-up. |

---

## Desenho

### Backend (Go)

- **`internal/app/diagram.go`** (novo): recebe, exportadas, `BuildSequenceDiagramPrompt(f store.Feature, spec string) string`, `SanitizeMermaid(raw string) (string, error)` + `ErrNotMermaidSequence`, `MermaidArtifactName(name string) string` (e o `extractFencedBlock` interno). Cópia fiel das funções do 0016, só promovidas de pacote.
- **`internal/cli/diagram.go`**: remove as definições movidas; `runSpecDiagram` passa a chamar `app.BuildSequenceDiagramPrompt`, `app.SanitizeMermaid`, `app.MermaidArtifactName`, `app.ErrNotMermaidSequence`. Sem mudança de comportamento.
- **`internal/server` (novo handler `handleGenerateDiagram`)**: rota `POST /api/features/{id}/diagram` em `server.New`. Fluxo (espelha `handleAsk`):
  1. Decodifica `{name?}`; `name := app.MermaidArtifactName(req.Name)`.
  2. `store.LoadFeature(repoDir, id)`; `ErrNotFound` → 404.
  3. Lê a spec vigente (`os.ReadFile(app.FeatureDocPath(repoDir, id, "spec.md"))`, ausência → "").
  4. `project.LoadConfig(projDir)`; se `delegation` (ou sem config) → 200 `{generated:false, needs_provider:true}`.
  5. `llm.NewProvider(cfg.LLM)`; `prov.Complete(ctx, app.BuildSequenceDiagramPrompt(f, spec))`.
  6. `app.SanitizeMermaid(body)`; `ErrNotMermaidSequence` → 422 (mensagem pt-BR como na CLI); erro de provider → 500.
  7. `app.SaveArtifact(repoDir, projDir, id, name, []byte(mermaid+"\n"))`.
  8. 200 `{generated:true, name, mermaid}`.

### UI

- **`api.ts`**: `generateDiagram(featureId, name?) → { generated, needs_provider?, name?, mermaid? }` (POST). Reusa `fetchLLMMode` (0019) para o gating.
- **`FeaturePage.tsx`** (`DiagramsSection`): botão **"Gerar com IA"** ao lado de "Novo diagrama".
  - Lê o modo (prop vinda do `FeaturePage`, que busca `fetchLLMMode`). Em `delegation`: botão desabilitado + dica ("a geração automática precisa do modo *assinatura* ou *chave própria*").
  - Ao clicar: estado `generating`; `generateDiagram(id)`. Em `generated:true` → reusa `editAsExcalidraw("<base>.mmd"≈name, mermaid)` (converte e abre o `DiagramPanel` com `initialScene`, nome `<base>.excalidraw`) e recarrega a lista (`load()`) para o `.mmd` novo aparecer. Em `needs_provider` → notice. Erros (422/500) → notice.
- Sem novas libs: `convertMermaidToScene` e o `DiagramPanel` já existem (0016). mermaid/excalidraw seguem lazy.

---

## Verificação

- **Unit (`internal/app`)**: testes movidos do 0016 — `SanitizeMermaid` (com/sem fence, prosa, não-sequence→`ErrNotMermaidSequence`), `BuildSequenceDiagramPrompt` (inclui id/título/módulo), `MermaidArtifactName` (slug, default, sufixo único).
- **Server (`internal/server`)** — `POST /api/features/{id}/diagram`:
  - `byo-key` (provider falso via `httptest`, como `ask_test`): `generated:true`, `name:"sequence.mmd"`, `mermaid` devolvido, artefato salvo em `content/artifacts/<id>/sequence.mmd` (+ commit, como `artifacts_test`).
  - `delegation`: `generated:false`, `needs_provider:true`, **nenhum** artefato escrito.
  - feature inexistente → 404.
  - saída não-sequence do modelo → 422.
- **CLI (`internal/cli`)**: `diagram_test.go` segue verde após o refactor (call sites apontando para `app`).
- **UI**: `tsc` + `npm run build` limpos; entry bundle não cresce (mermaid/excalidraw lazy). Manual: clicar "Gerar com IA" → Excalidraw abre preenchido; em `delegation` o botão fica desabilitado com dica.

---

## Fora de escopo / follow-ups

- Diagramas além de sequência (flowchart/class) — como no 0016.
- Geração no mux multi-projeto (`cerne serve`, case 0022) — exige expor artifacts/geração lá primeiro.
- Regenerar o `.mmd` reconciliando com um `.excalidraw` já bifurcado (edição segue one-way, herdado do 0016).
- Escolher o nome do diagrama antes de gerar (hoje default `sequence`); pode virar um campo depois.
