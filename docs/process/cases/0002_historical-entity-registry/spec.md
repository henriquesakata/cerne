# Case 0002 — Registro histórico de entidades

tags: #spec #cerne #domain #lint #import

**Status:** Draft — spec fechada via brainstorming (2026-06-13); pronta para `plan.md`.
**Tipo:** Extensão do modelo de domínio (novo `Kind`) + store + lint + import/CLI.
**Origem:** Pendência nº 1 do [result.md do case 0001](../0001_founding-spec/result.md). Surgiu da rodada de aceitação real contra o Rockt Hub (2026-06-12): o lint flagrou 18 referências reais a entidades supersedidas, mas **não** flagrou as menções a "Bills"/"Investments" — porque a absorção desses módulos pelo Finance **precede** a convenção de cases do hub, então não existe entidade `cerne` para supersedir e, sem entidade, não há needle para o lint casar.

---

## Problema

O lint `ref-to-superseded` (regra determinística do case 0001) só consegue flagrar menções a uma entidade quando:
1. existe uma entidade `cerne` (Feature/Decision/Reference) com estado terminal (`superseded`/`rejected`/`obsolete`), e
2. essa entidade tem needles deriváveis (ID `cerne`, título ≥ 4 chars, ou identificadores originais via `imported_from`).

Quando algo foi **absorvido/descontinuado antes de existir documentação importável** (caso Bills → Finance, Investments → Finance), não há entidade para registrar a supersedência. As menções vivas (`docs/planning/features.md` ainda lista Bills/Investments como módulos; `R-0010` desenha `bills_db`/`investments_db` na arquitetura) passam despercebidas — exatamente o tipo de obsolescência que o produto promete pegar.

Generalizando: **falta um caminho para declarar "a entidade X existiu e foi supersedida/absorvida por Y", mesmo quando X nunca teve spec/case próprio.**

---

## Objetivo e critérios de sucesso

Permitir registrar entidades **históricas** (existiram, foram absorvidas/descontinuadas) com seus identificadores originais (**aliases**), de modo que o lint passe a flagrar referências vivas a elas — sem exigir spec/case próprio nem inventar ciclo de vida que nunca houve.

**Critério de aceite** (fecha o critério nº 1 do case 0001 por completo):

1. Após registrar Bills e Investments como entidades históricas supersedidas pelo Finance (aliases `bills`/`boletos`, `investments`/`investimentos`), `cerne lint` no hub flagra as menções em `features.md` e em `R-0010` como `ref-to-superseded`.
2. Entidades históricas **não** aparecem na materialização `.docs/` (são history), mas **aparecem** em `cerne search` e `cerne status --all`.
3. Registro via comando avulso (`cerne retire`) **e** via seed em lote (`historical.toml` lido pelo `import`) produzem entidades idênticas (mesmo caminho de domínio).
4. Suíte verde, `go vet` limpo; cobertura nova por testes table-driven com erros pinados via `errors.Is`.

---

## Decisões do brainstorming (2026-06-13)

| Fork | Decisão | Consequência |
|---|---|---|
| **Modelo de dados** | **Novo `Kind` `historical`** (não reusar Feature/Decision; não ser só tabela de aliases) | Separa "isto é registro histórico" do fluxo normal; schema mínimo próprio |
| **Alvo do `superseded_by`** | **Polimórfico**: ID `cerne` (validado/linkável) **ou** string livre (label de módulo, ex.: "Finance") | Casa com legado onde o alvo pode não ser entidade |
| **Caminho de entrada** | **Comando + seed**: `cerne retire` é o primitivo; seed `historical.toml` é wrapper batch sobre o **mesmo** caminho de domínio | Cobre ad-hoc e onboarding de legado sem duplicar lógica |
| **Safety de alias** | **Validar na criação** (`ErrAliasTooShort`, mín. 5 chars) + lint casa com **word-boundary + case-insensitive** | Falha cedo; aliases ruins nunca persistem; reusa o guard do case 0001 |
| **Envelope de escopo** | **A — registro enxuto**: SEM `cerne lint --history-gaps` nesta rodada | Superfície mínima; descoberta de candidatos fica como passo futuro |

---

## Desenho

### Modelo de dados — `Kind` `historical`

Quarto `Kind` ao lado de `feature`/`decision`/`reference`. Prefixo de ID **`H-`** (`H-0001`, …), via `NextID`.

Campos (JSON, persistido pelo `store` como as demais entidades):

| Campo | Tipo | Nota |
|---|---|---|
| `id` | string | `H-NNNN` |
| `kind` | string | `historical` |
| `name` | string | nome original (ex.: `Bills`); serve de needle (título) no lint |
| `aliases` | `[]string` | identificadores originais (ex.: `bills`, `boletos`); cada um needle adicional |
| `superseded_by` | string | **polimórfico**: ID `cerne` (ex.: `F-0123`) ou label livre (ex.: `Finance`) |
| `reason` | string | por que foi absorvido/descontinuado (obrigatório — é uma supersedência) |
| `state` | string | sempre `superseded` (nasce terminal) |
| `imported_from` | string | opcional; proveniência (id/origem no doc tree legado) |
| `created_at` | time | como nas demais entidades |

**Nasce terminal, sem FSM e sem gates**: uma entidade histórica não tem spec, não transiciona. Setar `state = superseded` faz a entidade fluir pela maquinaria existente de `ref-to-superseded` no lint e pelos filtros de materialização/status com mudança mínima.

### `superseded_by` polimórfico

Armazenado como string. Resolução: se casa o padrão de ID `cerne` **e** resolve para uma entidade existente, é tratado como **link** (navegável na UI, validável no lint); caso contrário é um **label livre**. Validação na criação: não-vazio. (Sem checagem de existência forçada — o alvo legado pode ser só um módulo.)

### Caminhos de entrada

**Comando (primitivo):**

```
cerne retire <name> --by <id|label> --reason <texto> [--alias <a>]... [--imported-from <orig>]
```

Cria uma entidade `H-NNNN` (`kind=historical`, `state=superseded`), sem exigir spec/gate. Valida `name` (não-vazio), `reason` (não-vazio), `--by` (não-vazio) e cada `--alias` (≥ `MinAliasLen`). Strings de ajuda/erro do CLI em **pt-BR**.

**Seed (batch):** arquivo `historical.toml` (TOML — evita nova dependência; `BurntSushi/toml` já está no projeto), **auto-detectado pelo `import`** na raiz da árvore importada. Cada entrada é funilada pelo **mesmo** núcleo de domínio do comando:

```toml
[[historical]]
name          = "Bills"
aliases       = ["bills", "boletos"]
superseded_by = "Finance"
reason        = "Módulo de boletos absorvido pelo Finance antes da convenção de cases."

[[historical]]
name          = "Investments"
aliases       = ["investments", "investimentos"]
superseded_by = "Finance"
reason        = "Módulo de investimentos absorvido pelo Finance."
```

### Lint — needles de alias

Estender a derivação de needles do `ref-to-superseded`: além de ID, título (`name`) e `imported_from`, derivar needles de **`aliases`**. Guard reusado/endurecido: comprimento mín. (`MinAliasLen = 5`), **word-boundary**, **case-insensitive**. Como aliases já são validados na criação (`ErrAliasTooShort`), o lint não precisa filtrar comprimento em runtime — mas mantém word-boundary/case-insensitive para casar `bills`/`Bills`/`BILLS` sem casar `billships`.

Entidades `kind=historical` (estado `superseded`) entram no escopo do scan exatamente como as demais terminais.

### Materialização / search / status

- **`pull` / materialização:** entidades históricas são `state=superseded` → **excluídas** de `.docs/` como qualquer terminal (confirmar/assertar; ajustar exclusão se preciso incluir o `Kind`).
- **`cerne search`:** indexadas (FTS5 cobre histórico completo) → **encontráveis** por nome/alias.
- **`cerne status`:** o default (ativos) **não** lista históricas; `cerne status --all` **lista**, sob rótulo pt-BR próprio (ex.: "Históricas").

### Eventos

Novo evento de criação no log NDJSON: `historical.recorded`, com `{id, name, aliases, superseded_by, reason, source}` onde `source ∈ {"cli","import"}`. Sem eventos de transição (a entidade nasce terminal).

---

## Fora de escopo (deferred)

- **`cerne lint --history-gaps`** (relatório determinístico de termos frequentes em docs vivas sem entidade casada → candidatos a histórico). Útil para achar o *próximo* Bills; fica para um case futuro por risco de heurística/ruído. Decisão de escopo A.
- **Config de alias em `.cernerc`** (min-length/denylist por projeto): `MinAliasLen` fica hardcoded em 5 por enquanto.
- **Wiki-links `[[nota]]`** no lint (pendência nº 2 do case 0001) — ortogonal, case próprio.

---

## Questões em aberto — resolvidas

1. **Alias é atributo ou tabela separada?** → Atributo (`aliases []string`) do novo `Kind`, não tabela.
2. **O que `--historical`/retire relaxa?** → Não há gate: a entidade nasce `superseded` sem spec; evento `historical.recorded`.
3. **Ruído de aliases curtos/genéricos?** → `ErrAliasTooShort` (≥ 5) na criação + word-boundary/case-insensitive no lint.
4. **`--history-gaps`?** → Fora de escopo nesta rodada (envelope A).

---

## Plano de implementação (ordem TDD — esboço para `plan.md`)

1. **`internal/domain`**: `Kind` `historical`, prefixo `H-`, `MinAliasLen`, sentinels (`ErrAliasTooShort`, `ErrReasonRequired`, `ErrSupersededByRequired`), construtor que nasce `superseded` sem gates. Puro, table-driven.
2. **`internal/store`**: persistência da entidade histórica, `NextID` para `H-`, evento `historical.recorded`.
3. **`internal/app`**: núcleo compartilhado `RecordHistorical` (chamado por CLI **e** import).
4. **`internal/lint`**: needles a partir de `aliases` (word-boundary, case-insensitive); históricas no escopo do `ref-to-superseded`.
5. **`internal/cli`**: comando `cerne retire` (strings pt-BR) + auto-detecção de `historical.toml` no `import`.
6. **`internal/materialize`** + **status/search**: assertar exclusão de históricas de `.docs/`; inclusão em `search` e `status --all` (rótulo pt-BR).
7. **Aceitação no hub**: registrar Bills/Investments (via seed) e re-rodar `cerne lint` → confirmar achados em `features.md` e `R-0010`.

## Próximos passos

1. `plan.md` (skill `writing-plans`) detalhando milestones/commits TDD na ordem acima.
2. Implementação subagente-dirigida.
3. `result.md` com números reais da re-rodada de aceitação no hub.
