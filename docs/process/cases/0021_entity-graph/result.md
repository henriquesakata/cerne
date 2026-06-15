# Resultado — Case 0021 (grafo explicativo de entidades)

**Status:** Done — A/B/C entregues (2026-06-14); explicação por LLM (Frente D) adiada para follow-up.

---

## O que foi entregue

### Pesquisa (Understand Anything)
Clonado o fonte (MIT): UA é um plugin do Claude Code que renderiza com **React Flow** (`@xyflow/react`) + **elkjs** (layout em camadas) + graphology/Louvain (clustering). Suas arestas são `{type, direction, description, weight}` — o `description` (motivo, gerado por LLM) é o que as torna *explicativas*. **Diferença-chave:** o LLM do UA **inventa** as arestas a partir de prosa (pode alucinar). O cerne já tem as arestas como **verdade estruturada** com motivo escrito por humano → decisão: projetar arestas reais; LLM, se entrar, **explica, nunca cria** (Frente D).

### Backend (determinístico, sem LLM)
- `internal/app/graph.go` — `BuildGraph(repoDir)` projeta nós (feature/decision/reference/historical, com kind+state) e arestas:
  - `superseded_by` carregando **`Edge.Reason` = `store.Reason`** (motivo obrigatório no supersede — a carga explicativa, de graça);
  - `informs` (`Reference.Informs` → F/D);
  - `links` (citações markdown `[x](y)` entre content-docs que resolvem a um ID conhecido).
  - Arestas para alvo inexistente são descartadas (nunca pendura); saída determinística (nós por ID, arestas por from/to/type); slices não-nulas para JSON estável.
- `internal/mdlink` — pacote-folha novo com `ContentLinks(content, resolve)`, extraído do `lint` (o `broken-link` e o grafo consomem o mesmo primitivo; mora **abaixo** dos dois para evitar o ciclo app→lint via teste white-box do lint). `broken-link` mantém comportamento idêntico.
- `cerne graph --format json|dot` (`internal/cli/graphcmd.go`).
- `GET /api/graph` (`internal/server`) → `{nodes, edges}`.

### UI explicativa
- `#/graph` (`ui/src/Graph.tsx`), **lazy** como o Excalidraw: React Flow + **elkjs layered (DOWN)**; `EDGE_STYLES` por tipo (âmbar sólida / tracejadas teal e cinza), seta por aresta; clicar um nó **foca** (esmaece o resto, revela os rótulos das relações) e abre um **painel de conexões** com o **motivo** da substituição + link para a página da entidade; toggle "mostrar substituídas"; legenda; MiniMap por kind. Deps novas `@xyflow/react` + `elkjs`; `ui/dist` reconstruído (chunk do grafo isolado, fora do bundle principal).

**7 commits:** docs (spec+plan) → BuildGraph (estruturadas) → mdlink → links → CLI → server → UI.

## Aceitação
- `go vet ./...` limpo; suíte **450 → 457** (BuildGraph estruturadas/links, `mdlink.ContentLinks`, 3× CLI, 1× server endpoint). Build da UI ok (tsc verde); chunk `Graph-*.js` separado (gzip ~488 kB), só baixado em `#/graph`.
- **Motivo na aresta ponta-a-ponta:** `superseded_by` carrega `store.Reason` em dado → CLI (DOT label) → HTTP → painel da UI.

## Débitos e próximos passos
1. **Frente D — explicação por LLM** (`POST /api/graph/explain` + botão): adiada; o grafo determinístico está completo sem ela. LLM narraria arestas reais, nunca as criaria.
2. **`[[wiki-links]]`**: ainda não parseados (gap de lint #4); um case próprio que alimentaria lint **e** grafo de uma vez.
3. **Clustering (Louvain/`graphify`)**: aditivo; o DAG em camadas já dá legibilidade no v1.
4. **Layout:** o ELK recebe *todas* as arestas; `informs`/`links` podem distorcer as camadas. Refinar para a espinha de substituição definir a hierarquia (e cruzadas só desenhadas) é futuro.
5. **Smoke real do `cerne ui`** pendente (exige projeto com backend materializado); o teste httptest cobre o contrato JSON que a UI consome.
6. **Peso do bundle do grafo** (elkjs ~1,6 MB): mitigado pelo lazy; `manualChunks` se incomodar.
