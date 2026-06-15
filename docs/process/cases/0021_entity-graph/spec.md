# Case 0021 — Grafo explicativo de entidades

tags: #spec #cerne #ui #graph #explainability #navigation

**Status:** Done (2026-06-14) — A/B/C entregues; ver [result.md](result.md). Explicação por LLM (Frente D) adiada.
**Tipo:** Projeção determinística do grafo latente das entidades (feature/decision/reference/historical) em `app.BuildGraph`, exposta por `GET /api/graph` + `cerne graph`, e renderizada como **grafo explicativo** (React Flow, arestas tipadas + com motivo, layout em camadas) na UI. Núcleo sem LLM; explicação em linguagem natural é camada **opcional** sobre arestas que já são verdade.
**Origem:** Pedido do usuário (2026-06-14): não um "monte de bolinhas" topológico estilo Obsidian, mas um grafo **que explique como conceitos/regras se conectam**, inspirado no *Understand Anything* (https://understand-anything.com/, fonte MIT em github.com/Egonex-AI/Understand-Anything). Constatação do brainstorming: **os dados já são um grafo — e as arestas já são semânticas**: `superseded_by` carrega um **motivo obrigatório** (`store.Reason`, exigido em `domain.ValidateSupersedePayload`), `informs` diz que uma fonte motivou uma feature/decisão, `links` é citação. Falta projetar, rotular e renderizar de forma explicativa.

---

## Problema

Um grafo só de topologia (força-direcionado, nó↔nó sem rótulo) responde "o que se liga a quê" mas não "**por quê / como**". O usuário quer o segundo: ver que `F-007` foi **substituída por** `F-042` **e ler o motivo** ali, que `R-0042` (artigo externo) **embasou** `D-0007`, que a cadeia de decisões que levou ao estado atual tem uma forma legível — não um novelo. Hoje o cerne nem o topológico mostra: `/api/state` devolve três arrays sem arestas; o `lint` parseia os links markdown só para flagrar os quebrados (`mdLinkRx`, `internal/lint/lint.go`), descartando os vivos; e o **motivo da substituição** — que já é dado estruturado obrigatório na entidade (`store.Reason`) — não aparece em lugar nenhum de forma navegável.

**Vantagem do cerne sobre o Understand Anything:** lá um pipeline de LLM **inventa** as arestas a partir de prosa (e pode alucinar conexões). Aqui as arestas são **verdade estruturada** com motivo escrito por humano. Logo: o LLM, se entrar, **explica** arestas reais — nunca as **cria**. Pegamos o efeito explicativo do UA sem o risco de alucinação.

---

## Objetivo e critérios de sucesso

1. **Grafo determinístico (app-layer):** `app.BuildGraph(repoDir) (Graph, error)` carrega todas as entidades vigentes **e** terminais e emite `{Nodes, Edges}`. Sem LLM, sem rede. Reusa a extração de links do `lint` para promover links de conteúdo vivos a arestas.
2. **Arestas tipadas + com motivo** (o que torna o grafo *explicativo*), todas a partir do que já existe:
   - `superseded_by` — Feature→Feature, Decision→Decision, Historical→entidade atual. **`Edge.Reason` = `store.Reason` da entidade** (motivo obrigatório no supersede). É a aresta explicativa-chave, de graça.
   - `informs` — Reference→(Feature|Decision), via `Reference.Informs`. Rótulo "informa"; sem motivo por aresta.
   - `links` — Feature/Decision/Reference → entidade, via links markdown `[x](y)` do content-doc que **resolvem** para um ID existente. Rótulo "cita".
   - Direção é inerente (`From→To`); seta no render. Os links que não resolvem continuam sendo `broken-link` do lint, não viram aresta.
3. **Endpoint + CLI:** `GET /api/graph` e `cerne graph [--format json|dot]` → `{nodes:[{id,kind,title,state}], edges:[{from,to,type,reason}]}`. `kind ∈ {feature,decision,reference,historical}`; `type ∈ {superseded_by,informs,links}`.
4. **UI explicativa** (`#/graph`, `Graph.tsx`, **lazy**) — padrão *Understand Anything* (código MIT reaproveitável):
   - **React Flow (`@xyflow/react`)** para desenhar; **layout em camadas (`elkjs` `layered`, direção descendente)** para a espinha de substituição (que é um **DAG**), com `informs`/`links` como arestas cruzadas curvas. Não força-direcionado puro — a hierarquia *é* parte da explicação.
   - **Codificação visual por tipo de aresta** (cor/traço/seta): `superseded_by` âmbar sólida, `informs` tracejada, `links` cinza tênue (reusar o mapa `EDGE_STYLES` do UA).
   - **Rótulo da relação pintado na aresta** ao focar um nó (demais esmaecidas), estilo UA.
   - **Painel do nó** (clique): título, kind, state e lista **"Conexões"** com rótulos direcionais — e, para `superseded_by`, **o motivo renderizado** (o `description` que o UA gera por LLM, aqui é dado real).
   - Clicar nó navega para a página da entidade (`#/f|d|r/<id>`); cor do nó por kind; nó esmaecido em state terminal; órfãos visíveis.
5. **Explicação por LLM — opcional, atrás do provider existente** (`ask`/`compile`): ação sob demanda "explique esta conexão / este cluster / esta cadeia de substituição" → glosa em linguagem natural. **O LLM explica; nunca cria arestas.** Fiel a "núcleo determinístico, LLM plugável". Pode ficar para Frente D / follow-up — o grafo determinístico é completo sem ela.
6. Suíte + `go vet` verdes; build da UI ok (deps novas: `@xyflow/react` + `elkjs`).

---

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| **Topológico vs explicativo** | **Explicativo**: arestas tipadas + com motivo + layout em camadas, à la *Understand Anything* | É o pedido literal ("explique como se conectam"). cerne já tem o dado semântico; faltava render. |
| **Render** | **`@xyflow/react` (React Flow v12)** + `elkjs` layered | É o que o UA usa; casa com React 19/Vite/`dist` embutido. Suporta rótulo/estilo por aresta e painel de nó — força-direcionado puro (`react-force-graph`) não dá o efeito explicativo. |
| **Reuso de código** | Reaproveitar `EDGE_STYLES` + padrão "rótulo só nas arestas do nó focado" do UA (**MIT**) | Licença permite; encurta a UI. Atribuir no código. |
| **Motivo da aresta** | `Edge.Reason` = `store.Reason` da entidade (supersede/historical) | Dado já obrigatório e persistido (`transition.go`, `event.go`). UA gera isso por LLM; cerne tem de graça e confiável. |
| **LLM** | **Explica, nunca cria** arestas; atrás do provider, opcional | Arestas são verdade estruturada (≠ UA, onde a LLM inventa). Sem alucinação de conexão. Mantém núcleo determinístico. |
| **Layout** | DAG em camadas (`elkjs`) para `superseded_by`; `informs`/`links` cruzando | Cadeia de substituição é hierárquica — a forma é a explicação; novelo de força esconde isso. |
| **Modelo/persistência** | Projetar on-the-fly em `BuildGraph`; **sem** tabela no índice | Dezenas–centenas de entidades: montar é barato. Adjacência materializada = otimização de escala → outro case. |
| **`[[wiki-links]]`** | **Fora do v1** | Gap aberto do lint (CLAUDE.md próximos passos #4); parseá-los alimenta lint **e** grafo → case próprio. |
| **Clustering (Louvain/graphify)** | **Fora do v1**, aditivo depois | UA usa para domar hairball; o DAG em camadas já dá legibilidade no v1. |

---

## Desenho

### Backend (Go)
- `internal/lint/lint.go`: extrair de `linkFindings` (≈l.401) um helper puro `ContentLinks(content string, resolve func(target string) (id string, ok bool)) (resolved, broken []string)` — parse com `mdLinkRx`, particiona resolvível/quebrado; `linkFindings` passa a consumi-lo (comportamento idêntico).
- `internal/app/graph.go` (novo): `Node{ID,Kind,Title,State string}`, `Edge{From,To,Type,Reason string}` (`Reason` vazio para `informs`/`links`), `Graph{Nodes []Node; Edges []Edge}`; `BuildGraph(repoDir) (Graph, error)`:
  - `LoadAll*` (features/decisions/references/historical) → nós.
  - estruturadas: `SupersededBy`≠"" → `{from,to,"superseded_by",Reason:ent.Reason}`; cada `Reference.Informs[i]` → `{ref,to,"informs",""}`.
  - conteúdo: por content-doc (`content/<kind>s/<ID>.md`), `lint.ContentLinks` resolvendo path→ID; resolvidos e ≠self → `{ID,target,"links",""}`.
  - dedup; alvo inexistente não vira aresta.
- `internal/server`: `GET /api/graph` → `app.BuildGraph` serializado `{nodes,edges}`.
- *(opcional, Frente D)* `POST /api/graph/explain` `{from,to}` ou `{path:[ids]}` → glosa via provider `ask`; só quando há provider configurado.

### CLI
- `cerne graph` (`internal/cli/graphcmd.go` + `root.go`): `--format json|dot`; `dot` emite `digraph` (nós `ID title`, arestas rotuladas por type; motivo como tooltip/label do supersede).

### UI
- `package.json`: `@xyflow/react`, `elkjs`.
- `api.ts`: `fetchGraph()` + tipos `{nodes,edges}` (`edge.reason`).
- `App.tsx`: rota `#/graph` (lazy) + item no nav.
- `Graph.tsx`: React Flow; layout `elkjs` `layered` (DOWN) para `superseded_by`, demais cruzando; `EDGE_STYLES` por tipo (cor/traço/seta) — adaptado do UA (MIT, atribuído); rótulo da relação nas arestas do nó focado, demais esmaecidas; **painel do nó** com kind/state + "Conexões" (rótulo direcional + **motivo** quando `superseded_by`); `onNodeClick`→`#/f|d|r/<id>`; toggles (por kind, ocultar substituídas). *(opcional)* botão "explicar" → `POST /api/graph/explain`.

---

## Verificação
- **app (TDD):** fixtures com F substituída (com motivo), R com `informs`, D com link `[x](y)` vivo + um quebrado, historical→atual → `BuildGraph` emite nós certos e arestas `superseded_by`(com `Reason`)/`informs`/`links`; quebrado não vira aresta; self-link ignorado; dedup.
- **CLI:** `cerne graph` (json) bate com `BuildGraph`; `--format dot` parseável.
- **server (httptest):** `GET /api/graph` 200 + `{nodes,edges}` com `reason` no supersede.
- **UI:** build ok com deps novas; render em camadas, estilo por tipo, rótulo no foco, painel com motivo, clique navega (revisão + build).
- *(se Frente D)* `explain` só responde com provider configurado; degrada limpo sem.
- `go vet` + suíte verdes.

---

## Fora de escopo
- **LLM criando arestas** (o que o UA faz): aqui arestas são verdade estruturada; LLM só explica.
- **`[[wiki-links]]`** (Obsidian-style): case próprio que fecha também o gap de lint #4.
- **Clustering/comunidades** (Louvain/`graphify`): aditivo depois; DAG em camadas basta no v1.
- **Tabela `relationships` materializada** no índice SQLite: otimização de escala; v1 monta on-the-fly.
- **Editar estado pelo grafo / posições salvas:** leitura-só; layout recalcula a cada abertura.
