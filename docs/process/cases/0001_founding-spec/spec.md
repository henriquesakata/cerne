# Case 0001 — Spec fundadora do cerne

tags: #spec #cerne #founding #docs-as-code #ai-agents

**Status:** Done — v1 entregue (v0.2.0, 2026-06-12); ver [result.md](result.md)
**Tipo:** Spec fundadora — define o produto, a arquitetura e o corte do v1.
**Origem:** Brainstorming em 2026-06-12, motivado pela dor real do Rockt Hub: agentes de IA se perdendo em documentação histórica e citando estruturas que não existem mais.

---

## Problema

Projetos de software desenvolvidos com agentes de IA acumulam documentação **append-only**: specs, planos, resultados, ADRs e notas crescem sem parar, mas nada distingue mecanicamente o que está **vigente** do que é **histórico**. Consequências observadas no projeto de referência (Rockt Hub, ~6 meses de desenvolvimento intensivo com agentes):

1. **Escala** — ~1.000 arquivos markdown (786 em `docs/`, 226 em `apps/*/docs/`), incluindo 241 cases cross-app e 46 ADRs. Nenhum agente consegue (nem deve) ler isso para obter contexto.
2. **Estado é prosa, não dado** — "quais features foram criadas, quais implementadas, quais pendentes?" não tem resposta sem ler dezenas de arquivos e interpretar texto livre.
3. **Índices manuais mentem** — o `features.md` do hub ainda lista Bills e Investments como módulos separados, sendo que ambos foram absorvidos pelo Finance. O documento que deveria ser o mapa do estado atual é, ele mesmo, fonte de informação obsoleta. Agentes que o leem "aprendem" a estrutura errada e a propagam.
4. **Histórico e corrente têm o mesmo peso** — um case rejeitado de 2025 custa os mesmos tokens de leitura que a spec aprovada ontem. O agente não tem como saber a diferença.

O problema não é específico do hub: **qualquer projeto que use IA no desenvolvimento e documente o processo vai chegar aqui**. Ferramentas existentes não resolvem:

- **RAG** recupera trechos relevantes, mas não tem noção de estado (recupera o obsoleto com a mesma confiança que o vigente).
- **Jira/Notion/Confluence** têm estado, mas não têm camada de consumo para agentes (não materializam contexto em arquivos) nem amarração com o repositório de código.
- **LLM Wiki (Karpathy, 2025)** propõe a camada compilada de síntese mantida por LLM, mas não tem eventos estruturados de ciclo de vida — a detecção de obsolescência depende de passes de lint por LLM.

**A tese do cerne é a união dos três:** ciclo de vida estruturado (estado como dado) + camada compilada para consumo de agentes (wiki) + materialização do estado vigente em arquivos no repo do usuário.

> O nome: *cerne* é o lenho duro no centro da árvore — a parte durável que permanece enquanto a árvore cresce. A ferramenta preserva o cerne do projeto; o resto é histórico consultável.

---

## Visão do produto

**cerne** é uma ferramenta independente de plataforma (CLI + UI local, um único binário, open source em potencial) que gerencia a documentação viva de um projeto de software:

- O repositório do usuário ganha uma pasta **`.docs/` materializada e gitignored** contendo *apenas o estado vigente*: specs ativas, ADRs ativas, references vigentes e uma wiki compilada. É o que agentes leem — nativamente, sem protocolo.
- O **histórico completo** mora no backend do cerne (v1: um repositório git separado), com busca full-text local.
- **Transições de estado** (aprovar, rejeitar, substituir) são operações validadas da ferramenta — nunca edição de texto.
- Um **`docs.lock` commitado** amarra cada estado do código ao snapshot de docs correspondente.

Públicos: times que desenvolvem com agentes (Claude Code, Cursor, etc.) **e** criadores não-técnicos/amadores — para estes, o fluxo opinativo da ferramenta É o guia metodológico ("sua spec foi aprovada; o próximo passo é o plano").

O Rockt Hub é o primeiro cliente (dogfooding via `cerne import`); o Studio da plataforma Rockt será, no futuro, um cliente da API do cerne para projetos da plataforma. O cerne **não depende** da plataforma.

---

## Decisões (validadas no brainstorming)

| # | Decisão | Alternativas descartadas |
|---|---------|--------------------------|
| 1 | **Produto independente**, open source em potencial; hub é o primeiro adotante | Ferramenta interna do hub; módulo do Studio |
| 2 | **Híbrido files + estado estruturado**: conteúdo em markdown, grafo de estado como dado de primeira classe; `.docs/` materializada (gitignored) + `docs.lock` commitado + backend remoto com histórico | Files-first puro (estado vira prosa de novo); database-first (lock-in, agente refém de API) |
| 3 | **Escrita híbrida**: conteúdo edita-se como arquivo + `push`; transições de estado só via comando validado | Tudo via arquivo (sem consistência); tudo via API (atrito para agentes) |
| 4 | **CLI-first**; MCP como casca fina no v2 | MCP-only (restringe clientes); CLI+MCP simultâneos no v1 (escopo) |
| 5 | **Ciclo de vida fixo e opinativo no v1**: a ferramenta é a metodologia. Feature como entidade central | Workflow configurável tipo Jira (motor de workflow ≠ produto de opinião) |
| 6 | **Núcleo determinístico + LLM plugável**: materialização, lifecycle, lint estrutural e busca funcionam sem LLM; compilação de wiki e Q&A são camadas opcionais | LLM embutido obrigatório (custo + barreira para amador) |
| 7 | **UI web local servida pelo próprio binário** (`cerne ui`); Excalidraw embarcado para revisão visual de specs | Sem GUI (amador não vive no terminal); SaaS desde o dia 1 (infra prematura) |
| 8 | **Backend v1 = git-como-backend** (repo git separado gerenciado pelo cerne): zero infra, histórico auditável de graça | Server self-hosted ou SaaS no v1 (ambos viram backends futuros atrás da mesma interface) |

---

## Modelo de domínio

Seis tipos de entidade. Todas têm **ID estável** e **status como dado** (nunca prosa):

| Entidade | Papel | Estados |
|---|---|---|
| **Project** | Raiz; define módulos/escopos (ex.: finance, identity) | — |
| **Feature** | Entidade central (work item) | `candidata → aprovada → em-desenvolvimento → implementada`; terminais alternativos: `rejeitada`, `substituída` |
| **Document** | Spec, plano ou resultado — sempre pertence a uma Feature (são estágios dela) | derivado do estado da Feature |
| **Decision (ADR)** | Decisão arquitetural | `ativa / substituída` |
| **Reference** | Arquitetura, runbooks, guias | `vigente / obsoleto` |
| **Artifact** | Diagramas `.excalidraw`, imagens — anexados a Documents | — |

**Relações com semântica:** `superseded_by` carrega motivo + link (ex.: Feature Bills → `superseded_by: finance-absorption, reason: "absorvida pelo Finance"`). ADRs linkam às Features que as motivaram. É o que transforma "nota de rodapé que agentes ignoram" em registro estruturado que o lint usa.

**Gates do ciclo (fixos no v1):**

- `aprovar` exige spec presente
- `iniciar desenvolvimento` exige plano
- `concluir` exige resultado

**Log de eventos append-only:** toda transição vira evento (`feature.approved`, `feature.superseded`, `adr.superseded`, …). O log alimenta:

1. **Lint determinístico** — "Bills foi substituída → N páginas a referenciam → flagrar". Sem LLM.
2. **Invalidação de wiki** — páginas compiladas afetadas são marcadas sujas para recompilação.

---

## Arquitetura

Um único binário, quatro camadas:

```
┌─────────────────────────────────────────────────┐
│  Interfaces:  CLI  │  UI web local  │  (MCP v2) │
├─────────────────────────────────────────────────┤
│  Núcleo determinístico                          │
│  lifecycle · materialização · lint · busca FTS  │
├─────────────────────────────────────────────────┤
│  LLM plugável (opcional)                        │
│  compilação de wiki · Q&A                       │
├─────────────────────────────────────────────────┤
│  Storage                                        │
│  cache local (SQLite) ←sync→ backend remoto     │
│  (git-backend v1 │ self-hosted v2 │ SaaS v2+)   │
└─────────────────────────────────────────────────┘
```

**Pegada no projeto do usuário:**

| Item | Versionado? | Conteúdo |
|---|---|---|
| `.docs/` | não (gitignored) | materialização: docs vigentes + wiki + `CONTEXT.md` |
| `docs.lock` | **sim** | snapshot-id do estado dos docs ↔ estado do código |
| `.cernerc` | sim | config: backend, escopo padrão, nome do projeto |

O `docs.lock` é a correção do maior risco do modelo "docs fora do git": sem ele, checkout antigo não sabe qual era o estado dos docs, e PRs não amarram código a spec. Com ele, `cerne pull` reconstrói a materialização de qualquer commit (analogia: `package.json`/lockfile vs `node_modules`).

### Layout da materialização

```
.docs/
├── CONTEXT.md            ← ponto de entrada único, gerado, com orçamento de tokens
├── wiki/                 ← síntese compilada: 1 página de visão por módulo
└── current/
    ├── features/         ← só candidatas/aprovadas/em-desenvolvimento (spec/plano)
    ├── decisions/        ← só ADRs ativas
    └── references/       ← só docs vigentes
```

**Contrato com o agente (1 parágrafo no CLAUDE.md do projeto):** *"Estado atual do projeto em `.docs/` — comece por `CONTEXT.md`. Não cite documentação fora de `.docs/`. Histórico via `cerne search` / `cerne ask`."*

**Promoção na conclusão (regra anti-crescimento):** spec, plano e resultado são documentos *de processo*. Quando a Feature vira `implementada`, eles saem da materialização (ficam no histórico); o conhecimento durável é promovido (decisões → ADR, comportamento → Reference, visão → wiki). O corrente não cresce com a idade do projeto — cresce só com o tamanho atual dele.

---

## Comandos (v1)

| Comando | Função |
|---|---|
| `cerne init` | projeto novo: cria backend, lock, `.docs/` |
| `cerne import` | repo existente: varre docs, propõe mapeamento (case→Feature, adr→Decision), popula backend |
| `cerne pull [--scope X]` | materializa `.docs/` (fatia opcional); idempotente |
| `cerne push` | valida schema/links, grava no backend, atualiza lock, lint incremental |
| `cerne status` | tabela de features por estado |
| `cerne feature new "título"` | cria Feature `candidata` |
| `cerne feature approve F-042` | valida gate (spec existe?), registra evento |
| `cerne feature supersede F-007 --by F-042 --reason "..."` | substituição estruturada |
| `cerne lint [--fix-tasks]` | relatório determinístico: referências a substituídos, links quebrados, órfãos, gates pendentes; `--fix-tasks` emite tarefas para o agente hospedeiro executar |
| `cerne search "query"` | FTS local sobre o histórico completo |
| `cerne ui` | abre dashboard local |

**Conflitos:** `push` detecta drift remoto (lock desatualizado) e exige `pull` antes — modelo git-like. Conteúdo é markdown e o backend v1 é git: merges difíceis caem em ferramentas existentes.

**v1.5 (LLM plugável):** `cerne compile` (recompila páginas wiki sujas) e `cerne ask "pergunta"` (Q&A: responde pela wiki, mergulha no histórico se preciso). Modos: BYO API key ou delegação ao agente hospedeiro.

---

## UI local (v1)

`cerne ui` serve em `localhost`:

1. **Kanban de features por estado** — resposta visual a "o que está criado/implementado/pendente"
2. **Página da feature** — abas spec/plano/resultado, diff contra a última versão aprovada
3. **Tela de aprovação** — spec + diagramas lado a lado; aprovar/rejeitar com motivo obrigatório (vira evento)
4. **Relatório de lint** e timeline de eventos

**Excalidraw:** componente React open source embarcado. O agente gera o JSON `.excalidraw` (diagrama de sequência, arquitetura) como Artifact anexado à spec; o humano visualiza e edita no embed antes de aprovar.

---

## Corte de versões

| v1 (MVP) | v1.5 | v2+ |
|---|---|---|
| CLI: init, import, pull, push, status, lifecycle, lint, search | `compile` + `ask` (LLM plugável) | MCP server |
| Backend git + lock + materialização | | Server self-hosted / SaaS |
| UI: kanban + revisão de spec + aprovação c/ Excalidraw | | Integração com Studio (Rockt) |

---

## Critérios de sucesso

1. **Teste de aceitação real — `cerne import` do Rockt Hub:** importa 241 cases + 46 ADRs propondo mapeamento correto; o lint flagra, sem intervenção, as referências a Bills/Investments como entidades substituídas (o bug real do `features.md`).
2. **Redução de contexto:** agente no hub passa de ~1.000 arquivos visíveis para dezenas (features ativas + ADRs ativas + references vigentes). O caso "agente cita Bills como módulo vivo" morre por construção.
3. **`cerne status` responde em 1 comando** o que hoje exige ler dezenas de arquivos.
4. **Golden path e2e:** `init → feature new → push spec → approve → pull` funciona num repo virgem em < 5 minutos, sem LLM e sem infra além de um repo git para backend.
5. **Rastreabilidade:** checkout de commit antigo + `cerne pull` reconstrói a materialização daquela época.

---

## Estratégia de testes

- O núcleo determinístico paga aqui: gates de lifecycle, snapshots de materialização e regras de lint são testes de unidade/integração puros, **sem mock de LLM**.
- E2e do golden path (critério 4).
- Dogfooding contínuo: o próprio repositório do cerne usa cerne (este case 0001 será importado como Feature F-001 quando o `import` existir).

---

## Questões em aberto

1. **Stack** — requisito "um binário, CLI + UI embarcada + SQLite" aponta para **Go ou Rust**; decidir no plan.md (critérios: velocidade de iteração, embed de assets web, FTS do SQLite, distribuição multiplataforma).
2. **Licença e idioma** — se open source: licença (MIT/Apache-2.0?) e idioma da documentação pública (inglês) vs docs de processo (pt-BR hoje). Decidir antes do primeiro release público.
3. **Nome no GitHub** — npm `cerne` livre em 2026-06-12; verificar org/repo no GitHub na criação do remoto.
4. **Multi-usuário** — v1 assume um usuário/agente por vez por projeto (lock otimista). Modelo de colaboração simultânea fica para o server v2.
5. **Granularidade do orçamento de tokens do `CONTEXT.md`** — heurística inicial vs configurável.

---

## Próximos passos

1. `plan.md` neste case (skill `writing-plans`): decidir stack, esqueleto do repo, ordem de implementação TDD (sugestão: lifecycle/estado → lock/materialização → import → lint → search → UI).
2. Implementar v1 com o hub como cobaia de `import` desde cedo.
3. `result.md` ao concluir; status desta spec → Done.
