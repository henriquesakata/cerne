# Case 0016 — `cerne spec diagram`: diagrama de sequência via assinatura + UI

tags: #spec #cerne #llm #diagram #mermaid #excalidraw #ui

**Status:** Draft → em implementação (2026-06-13)
**Tipo:** Comando de autoria de diagrama (LLM) + suporte a artifact Mermaid (backend/server) + renderização e edição na UI.
**Origem:** Pedido do usuário: no modo standalone, usar a assinatura Claude Code para **elaborar diagramas de sequência**. Conecta a visão original (case 0001: Excalidraw para revisar a spec com diagrama antes de aprovar) com o provider `claude-cli` (case 0014).

---

## Problema

O provider (case 0014) sabe fazer `Complete(prompt)` na assinatura, mas **não há comando que gere diagrama**, os artifacts são **só `.excalidraw`** (criados à mão na UI) e a UI não renderiza Mermaid. Falta: pedir o diagrama ao LLM, guardá-lo, vê-lo e editá-lo.

---

## Objetivo e critérios de sucesso

1. `cerne spec diagram <id>` gera um **diagrama de sequência em Mermaid** via o provider configurado (delegação imprime o prompt; byo-key/claude-cli geram e salvam) e o guarda como artifact `content/artifacts/<id>/<nome>.mmd`.
2. Backend/server aceitam e servem `.mmd` (texto), além de `.excalidraw` (JSON).
3. UI **renderiza** o `.mmd` (mermaid.js) na página da feature e permite **"editar como Excalidraw"** (conversão `mermaid-to-excalidraw` → editor existente, salvando um `.excalidraw`).
4. Na assinatura (claude-cli), sem API key — smoke real gera um diagrama de verdade.
5. Suíte + `go vet` verdes; build da UI ok (mermaid em chunk lazy).

---

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Formato do LLM** | **Mermaid `sequenceDiagram`** (não Excalidraw JSON) | LLM é confiável em Mermaid; Excalidraw JSON cru (coordenadas/ids) é frágil. |
| **Editável** | LLM gera `.mmd`; UI converte via **`@excalidraw/mermaid-to-excalidraw`** para o editor existente, salvando um `.excalidraw` irmão | Confiabilidade do LLM + edição no Excalidraw (visão do case 0001). `.mmd` é a fonte gerada; editar bifurca num `.excalidraw`. |
| **Comando** | `cerne spec diagram <id> [--name <n>]` (sob `spec`), default `sequence` → `sequence.mmd` | Junto de `spec draft`; coerente. |
| **Sanitização** | `sanitizeMermaid(raw)` remove cercas ```` ```mermaid ```` e prosa, valida início `sequenceDiagram` | LLM às vezes embrulha em fence/prosa. |
| **Server `.mmd`** | GET serve `text/plain; charset=utf-8`; PUT aceita `.mmd` (texto não-vazio) **ou** `.excalidraw` (JSON) | Generaliza o artifact handler (hoje `.excalidraw`-only). |
| **Delegação** | imprime o prompt + onde escrever + `cerne push` | Igual a compile/ask/spec-draft. |

---

## Desenho

### Backend (Go)
- `BuildSequenceDiagramPrompt(f, specContent)` (pt-BR): pede **um diagrama de sequência em Mermaid** (`sequenceDiagram`) para a feature (id/título/módulo), usando a spec como contexto; "responda apenas com o código Mermaid, sem cercas".
- `sanitizeMermaid(raw) (string, error)`: extrai de ```` ```mermaid ... ``` ```` se houver; trim; erro se não começar com `sequenceDiagram` (após trim) — sentinel `ErrNotMermaidSequence`.
- `cerne spec diagram <id> [--name]` (`internal/cli/speccmd.go`): resolveProject; LoadFeature; lê a spec (contexto); delegação → imprime prompt; senão `prov.Complete` → `sanitizeMermaid` → `app.SaveArtifact(repoDir, projDir, id, "<name>.mmd", []byte(mermaid))` (commit+push) → imprime "diagrama rascunhado e enviado".
- `internal/server/artifacts.go`: GET `.mmd` → `text/plain; charset=utf-8`; PUT valida `.excalidraw` (JSON, atual) **ou** `.mmd` (texto não-vazio, sem json.Valid).

### UI
- deps: `mermaid` (render) + `@excalidraw/mermaid-to-excalidraw` (conversão). Ambos **lazy**.
- `DiagramsSection` (FeaturePage): ramifica por extensão. `.excalidraw` → fluxo atual (DiagramPanel). `.mmd` → `MermaidView` (lazy) que busca o texto e renderiza; botão **"Editar como Excalidraw"** converte (`parseMermaidToExcalidraw`) e abre o `DiagramPanel` com cena inicial, salvando como `<base>.excalidraw`.
- `api.ts`: `fetchArtifactText` (reusa o fetch atual, devolve texto) para `.mmd`.

---

## Verificação

- **Unit:** `sanitizeMermaid` (com/sem fence, prosa, não-sequence→erro); `BuildSequenceDiagramPrompt` inclui id/título/módulo; `cerne spec diagram` byo-key (httptest) salva `<name>.mmd` + commit; server GET/PUT `.mmd`.
- **Smoke real (assinatura):** `mode="claude-cli"`, `cerne spec diagram F-0001` → Mermaid `sequenceDiagram` real gerado e salvo.
- **UI:** build ok; mermaid em chunk lazy; render do `.mmd` + botão de conversão (verificação por build + revisão; render no browser não automatizável aqui).

---

## Fora de escopo / staging

- Implementação em 2 frentes: **backend** (comando + artifact + server) primeiro, **UI** (render + conversão) depois — re-sincronizando com o trabalho concorrente de UI (cases 0011/0012/0015).
- Diagramas além de sequência (flowchart/class) — o comando é específico de sequência; o pipeline Mermaid generaliza depois.
