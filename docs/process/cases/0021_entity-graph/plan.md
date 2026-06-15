# Plano — Case 0021 (grafo explicativo de entidades)

**Goal:** projetar o grafo latente das entidades em `app.BuildGraph` (arestas tipadas `superseded_by`/`informs`/`links`, com **motivo** na substituição), expor por `GET /api/graph` + `cerne graph`, e renderizar como **grafo explicativo** estilo *Understand Anything* — React Flow + layout em camadas, estilo por tipo de aresta, rótulo da relação e painel do nó com o motivo. Núcleo determinístico; explicação por LLM é camada opcional que **narra arestas reais, nunca as cria**.

**Decisões resolvidas (eram abertas na spec):**
- Render: **`@xyflow/react` (React Flow v12) + `elkjs`** (layout `layered`), substituindo a ideia anterior de `react-force-graph` — força pura não dá o efeito explicativo. Reaproveitar `EDGE_STYLES` + padrão de rótulo-no-foco do UA (**MIT**, atribuir).
- Motivo da aresta: `Edge.Reason` = `store.Reason` (já persistido em `transition.go`/`decision.go`/`historical.go`; obrigatório no supersede).
- Reuso de parsing: helper compartilhado extraído do `lint`.
- Persistência: nenhuma — `BuildGraph` on-the-fly. Clustering/wiki-links/relationships-table: fora (ver spec).

## Frente A — núcleo: lint helper + app + CLI (TDD)

1. **Refactor `lint`** (`internal/lint/lint.go` + testes verdes): extrair de `linkFindings` (≈l.401) `ContentLinks(content string, resolve func(string)(string,bool)) (resolved, broken []string)`; `linkFindings` consome-o (comportamento idêntico). → commit `refactor(lint): extract reusable content-link resolver`.

2. **`app.BuildGraph` + tipos** (novo `internal/app/graph.go` + `graph_test.go`): `Node{ID,Kind,Title,State}`, `Edge{From,To,Type,Reason}`, `Graph{Nodes,Edges}`; `BuildGraph(repoDir)`:
   - `LoadAll*` → nós; `SupersededBy`≠"" → `superseded_by` **com `Reason: ent.Reason`**; `Reference.Informs[i]` → `informs`; content-doc via `lint.ContentLinks` resolvendo path→ID → `links`; dedup; alvo inexistente não vira aresta.
   - Fixtures: F substituída **com motivo**, R com `informs`, D com link vivo + um quebrado, historical→atual. Assert `Reason` presente só no supersede. → commit `feat(app): BuildGraph — typed/reasoned entity graph projection`.

3. **CLI `cerne graph`** (`internal/cli/graphcmd.go` + `root.go` + teste): `--format json|dot`; `dot` emite `digraph` (arestas rotuladas por type; motivo do supersede como label/tooltip). Teste: json bate com `BuildGraph`; dot parseável. → commit `feat(cli): cerne graph — export entity graph (json|dot)`.

Verificar: `go vet` + suíte verdes.

## Frente B — server (TDD)

4. **`GET /api/graph`** (`internal/server` + httptest): `app.BuildGraph` → `{nodes:[{id,kind,title,state}], edges:[{from,to,type,reason}]}`. Teste: 200 + `reason` no supersede, consistente com fixtures. → commit `feat(server): GET /api/graph`.

## Frente C — UI explicativa (React Flow)

5. **Deps + página** (`ui/`): `npm install @xyflow/react elkjs` (atualiza lockfile); `api.ts` `fetchGraph()` + tipos (`edge.reason`); `App.tsx` rota `#/graph` (lazy) + nav; `Graph.tsx`:
   - React Flow; layout **`elkjs` `layered` (DOWN)** para a espinha `superseded_by`, `informs`/`links` como arestas cruzadas curvas.
   - `EDGE_STYLES` por tipo (âmbar sólida / tracejada / cinza tênue) — adaptado do UA (MIT, **atribuir no header do arquivo**); rótulo da relação só nas arestas do nó focado (demais opacidade ~0.05).
   - **Painel do nó**: kind/state + "Conexões" (rótulo direcional + **motivo** quando `superseded_by`); `onNodeClick`→`#/f|d|r/<id>`; cor por kind; esmaecer states terminais; toggles (kind, ocultar substituídas).
   - `npm run build`; commitar `package.json` + `package-lock.json` + `ui/dist`. → commit `feat(ui): explanatory entity graph (React Flow, typed/reasoned edges)`.

Verificar: `go test ./...` (embute dist) verde.

## Frente D — explicação por LLM (opcional)

6. *(opcional, pode virar follow-up)* `POST /api/graph/explain` `{from,to}`|`{path:[ids]}` → glosa via provider `ask` (só com provider configurado; degrada limpo sem); botão "explicar conexão/cadeia" no painel da UI. **Narra arestas reais; não cria arestas.** → commit `feat(server,ui): on-demand LLM explanation of a graph connection`.

## Fechamento
- `result.md`; status spec → Done; linha 0021 no índice → Done; `update-cerne.ps1`; smoke real opcional (`cerne graph --format dot` no hub).
