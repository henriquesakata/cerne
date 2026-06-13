# Case 0009 — UI: navegação + páginas de leitura (decisions, references, busca, lint, history)

tags: #spec #cerne #ui #server

**Status:** Spec (rascunho — pré-plan.md, 2026-06-13)
**Tipo:** App shell de navegação (frontend) + 3 endpoints de leitura novos no servidor (search, lint, history) + 2 páginas que reaproveitam dados já existentes (decisions, references).
**Origem:** Observação do usuário ao rodar `cerne ui`: só o kanban e a página da feature existem. O backend já guarda decisions, references, busca full-text sobre todo o histórico, lint determinístico e o log de eventos — tudo alcançável pela CLI, nada pela UI. Este case torna o resto do backend **visível** na UI. (Renumerado de 0008 → 0009: o 0008 foi ocupado por `cache-auto-recreate` em paralelo.)

---

## Problema

A UI hoje é kanban + página da feature, e só. [`ui/src/App.tsx`](../../../../ui/src/App.tsx) é um hash-router pelado, **sem nenhuma navegação/chrome**: o kanban não tem como levar a lugar nenhum além dos cards, e a página da feature só volta pro kanban. Consequências:

1. **decisions e references já viajam no `GET /api/state`** ([`internal/server/server.go`](../../../../internal/server/server.go), `stateResponse`), mas o frontend descarta os dois campos — a informação chega no navegador e é jogada fora.
2. **Busca, lint e histórico** existem como comandos (`cerne search`, `cerne lint`, e o event log via `store.ReadEvents`) mas não têm endpoint HTTP nem página.
3. **Sem um shell de navegação, nenhuma página nova é alcançável** — esse é o pré-requisito compartilhado de tudo aqui (e do [case 0010](../0010_feature-authoring-ui/spec.md)).

O efeito prático: quem usa a UI acredita que o cerne "só tem kanban", quando na verdade o núcleo determinístico inteiro está pronto e invisível.

---

## Objetivo e critérios de sucesso

Expor na UI todo o estado de leitura que o backend já computa, atrás de uma navegação simples e estável, sem reescrever o roteamento nem adicionar dependências pesadas.

**Critério de aceite:**

1. **Shell de navegação**: barra persistente com Kanban · Decisions · References · Busca · Lint · Histórico; rota por hash para cada uma; item atual destacado; o kanban continua a rota default (`#/`). Nenhuma dependência nova de router (mantém o hash-router atual, só com uma tabela de rotas).
2. **Decisions**: lista todas as decisions agrupadas por estado (`active` / `superseded`), cada uma linkando para um detalhe que renderiza o doc de conteúdo (markdown) e mostra "substituída por" quando aplicável. **Zero backend novo de dados** (lista vem do `/api/state`; o detalhe usa um endpoint espelhando o de feature).
3. **References**: mesma forma para references (`current` / `obsolete`).
4. **Busca**: caixa de query → resultados do índice FTS5 sobre **todo o histórico** (inclui supersedidas/rejeitadas), cada hit mostrando id, tipo (feature/decision/reference), estado, snippet e — quando supersedido — o sucessor. Backed por `GET /api/search?q=&limit=`.
5. **Lint**: roda as regras determinísticas de staleness e renderiza os achados agrupados por regra (mesma ordem e rótulos da CLI, via `lint.RuleOrder`), com estado vazio claro ("Nenhum problema encontrado"). Backed por `GET /api/lint`.
6. **Histórico**: timeline do ciclo de vida a partir do event log (mais recente primeiro); cada evento = id da entidade, tipo, ator, timestamp e payload (`by`/`reason`). Backed por `GET /api/events`.
7. Suíte verde, `go vet` limpo; UI reconstruída (`ui/dist` commitado e embutido). Endpoints novos devolvem o mesmo contrato de erro pt-BR (`{"error": "..."}`) dos existentes.

---

## Decisões do design (2026-06-13)

| Fork | Decisão | Razão |
|---|---|---|
| **Roteamento** | Manter o hash-router atual; extrair uma pequena tabela de rotas + um componente de shell. **Sem** react-router | Superfície mínima (convenção do projeto); o router atual já é só um `switch` sobre o hash |
| **Detalhe de decision/reference** | Endpoints novos `GET /api/decisions/{id}` e `GET /api/references/{id}`, devolvendo entidade + markdown do doc de conteúdo, espelhando `handleFeatureDetail` | Simetria com a feature; o doc de conteúdo já é lido pela materialização/índice — mesmo caminho |
| **Busca** | `GET /api/search` embrulha `index.Search`; o servidor constrói o índice on-demand se faltar (como a CLI faz em `runSearch`), lendo `project.IndexPath` | Offline by design — lê o clone em cache + índice local, nunca o remoto; idêntico ao `searchcmd` |
| **Lint** | `GET /api/lint` embrulha `lint.Run(repoDir)` → JSON de achados; reusar `lint.RuleOrder` para ordenar | Read-only, determinístico; a UI só renderiza o que a CLI já imprime |
| **Histórico** | `GET /api/events` embrulha `store.ReadEvents(repoDir)`, devolvido do mais novo pro mais antigo | Reusa o leitor de NDJSON existente; lista plana v1 (filtro por entidade fica pra depois) |
| **Frescor do índice** | Servidor constrói o índice se faltar, mas **não** auto-atualiza a cada mutação | `cerne pull` é o ponto de sync (igual à CLI); auto-refresh é escopo de outro case |
| **Idioma** | Rótulos da nav e estados em pt-BR na UI; dado continua inglês | Convenção do projeto (data vs UI) |

---

## Desenho

### Servidor — endpoints novos (todos `GET`, read-only)

```go
// GET /api/decisions/{id}  → { decision, doc }   (espelha featureDetailResponse)
// GET /api/references/{id}  → { reference, doc }
// GET /api/search?q=<query>&limit=<n>  → { results: []index.Result }
// GET /api/lint  → { findings: []lint.Finding }   (achados na ordem de lint.RuleOrder)
// GET /api/events  → { events: []domain.Event }   (mais recente primeiro)
```

- `search`: resolve `project.IndexPath(cfg.ProjectID)`; se o arquivo não existe, `index.Rebuild(repoDir, dbPath)` antes de `index.Search` (mesma lógica de [`searchcmd.go`](../../../../internal/cli/searchcmd.go)). `q` vazio ⇒ 400 pt-BR.
- `lint`/`events`: leitura pura de `repoDir`; erro de I/O ⇒ 500 com a mensagem original (contrato atual).
- Registrar todos sob o mesmo `mux` de `server.New`, antes do fallback `/api/` → 404. O servidor passa a precisar do `ProjectID` (hoje `New(repoDir, projDir)` não o recebe) — estender a assinatura ou resolver via `project` a partir de `projDir`.

### Frontend — shell + páginas

- **`App.tsx`**: tabela de rotas `{ '#/': Kanban, '#/decisions': Decisions, '#/references': References, '#/search': Search, '#/lint': Lint, '#/history': History, '#/f/<id>': FeaturePage }`; um `<Nav>` persistente acima do `<main>`; rota desconhecida cai no kanban.
- **`api.ts`**: `fetchDecision`, `fetchReference`, `search(q, limit)`, `fetchLint()`, `fetchEvents()` + os tipos `DecisionDetail`, `SearchResult`, `LintFinding`, `Event` espelhando o JSON Go.
- **Páginas**: `Decisions`/`References` (lista agrupada por estado + detalhe markdown via `ReactMarkdown`, já usado em `FeaturePage`); `Search` (input + lista de hits); `Lint` (grupos por regra + total); `History` (linha do tempo).

---

## Verificação (corpus real do hub)

Rodar `cerne ui` contra o clone do hub e, no navegador: (a) decisions/references aparecem e abrem; (b) busca por um termo de uma feature **supersedida** retorna o hit com o sucessor; (c) lint mostra achados reais (o hub tem staleness conhecida); (d) histórico mostra o stream de eventos do ciclo de vida. Sem regressão no kanban/feature.

---

## Fora de escopo

- **Transicionar/editar** decisions e references pela UI (aqui são read-only; o ciclo delas segue na CLI). Autoria de features é o [case 0010](../0010_feature-authoring-ui/spec.md).
- Auto-refresh do índice de busca a cada mutação (mantém `cerne pull` como ponto de sync).
- Filtro de histórico por entidade / visão de diff.
- Páginas de wiki/`compile`/`ask` (camada LLM — território do [case 0003](../0003_llm-compile-ask/spec.md)).
- Paginação/virtualização de listas grandes (as 308 features do hub renderizam bem; revisitar se necessário).
