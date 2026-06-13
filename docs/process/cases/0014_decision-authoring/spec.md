# Case 0014 — Autoria nativa de decisions (ADRs)

tags: #spec #cerne #decision #adr #cli

**Status:** Spec — aguardando revisão (2026-06-13); plan.md e implementação a seguir.
**Tipo:** Nova superfície de CLI (`cerne decision`) ligando o FSM/persistência de decision que já existem; renomeação do verbo `feature supersede` → `feature substituir`.
**Origem:** Gap relatado pelo agente do Rockt Hub durante o dogfooding: o cerne só tem autoria nativa de `feature` (+`retire`); não há `decision new` nem transição de decision. Hoje ADRs só entram via `cerne import` (45 importados). A pergunta do agente: ADR novo continua *file-authored* ou vira *cerne-native*?

---

## Problema

A decision é entidade *first-class* em **tudo menos na autoria**:

| Camada | Decision? | Onde |
|---|---|---|
| Domain (FSM `active → superseded`) | ✅ | `internal/domain/decision.go` |
| Store (JSON, Save/Load/All) | ✅ | `internal/store/entity.go` |
| Importer (ADR `adr-NNN-*.md` → D-NNNN) | ✅ | `internal/importer/importer.go` (`applyDecision`) |
| Materialize (projeta só `active`) | ✅ | `internal/materialize/materialize.go` |
| Lint (flagra `superseded`, com aliases importados) | ✅ | `internal/lint/lint.go` (`buildNeedles`) |
| **CLI (criar / transicionar)** | ❌ | `root.go` registra `feature`/`retire`/…, nenhum `decision` |

Duas consequências:

1. **Autoria** — não existe comando para criar uma decision; ADRs só entram via `cerne import`. O import já é **idempotente** (pula docs cujo `imported_from` já existe — `importer.Unimported`), então *file-authored → re-import* funciona para o legado; mas isso não é **autoria**: o `D-NNNN` e o estado saem da heurística do import, não da intenção do autor — e o conteúdo do ADR novo passa a nascer no backend do cerne (decisão de concentrar a documentação ali, ver Decisões), não como arquivo solto em `docs/architecture/adr/`.

2. **Transição** — **não há nenhum caminho para substituir uma decision**. `domain.TransitionDecision` existe mas só é chamada por testes (código morto em produção). O único jeito de uma decision virar `superseded` hoje é o regex do import adivinhando a palavra no corpo (`reDecisionSuperseded`) ou edição manual do JSON — exatamente a "transição por edição de texto" que a spec fundadora proíbe: *"Transições de estado (aprovar, rejeitar, substituir) são operações validadas da ferramenta — nunca edição de texto"* (0001). O evento `adr.superseded` é nomeado na spec 0001 e **nenhum comando o emite**.

---

## Objetivo e critérios de sucesso

Dar à decision a mesma superfície de autoria e transição que a feature já tem, ligando o FSM e a persistência que já existem; o `import` (já idempotente) segue carregando os ADRs legados.

**Critério de aceite:**
1. `cerne decision new "<título>"` cunha D-NNNN (`NextID("D")`), salva a entidade em `active`, grava stub em `content/decisions/D-NNNN.md` com frontmatter `cerne_id`, emite `decision.created`, e faz commit+push+lock. Saída pt-BR.
2. `cerne decision substituir <id> --by <id> --reason "..."` aplica `domain.TransitionDecision(active, ActionSupersede)`, seta `state=superseded` + `superseded_by` + `reason` + `updated_at`, emite `decision.superseded`, commit+push+lock. `--by` e `--reason` obrigatórios; erro claro quando a decision (ou o `--by`) não existe, quando `--by` é ela mesma, ou quando já está `superseded`.
3. `cerne feature supersede` é renomeado para `cerne feature substituir` (mesma ação `domain.ActionSupersede`); ajuda e mensagens de erro/saída usam "substituir/substituída". Os demais verbos (approve/reject/start/complete) ficam inalterados.
4. **O dado permanece inglês**: valor `superseded` no JSON, campo `superseded_by`, identificador `domain.ActionSupersede`, evento `*.superseded`, regra `ref-to-superseded`. Só o verbo do CLI e os textos pt-BR mudam (ver memória `ptbr-formal-correctness` + regra *data-vs-UI* do CLAUDE.md).
5. **Downstream sem mudança de código, coberto por teste de ciclo via CLI**: materialize já some com a decision do `.docs/` ao substituí-la (filtra `state != active`); lint já flagra menção a decision substituída em doc vigente (`ref-to-superseded`).
6. `go vet` + suíte completa verdes; e2e cobre o golden path da decision (`new → substituir`, refletido no `pull`).

---

## Decisões do design (2026-06-13)

| Fork | Decisão | Razão |
|---|---|---|
| **Autoria do ADR novo** | **cerne-native** (`decision new` cunha entidade + stub) | Simetria com `feature new`: o autor controla o ID e o estado explícito, em vez de deixá-los para a heurística do import. A fonte da verdade do conteúdo do ADR passa a morar no backend (`content/decisions/`), materializada *read-only* no `.docs/` — como já é com specs de feature. (O `import` já é idempotente, então *file-authored* seria tecnicamente possível; ainda assim a autoria nativa é o modelo escolhido — concentra a documentação no backend e dá par à transição validada abaixo.) |
| **Verbo de transição** | **`substituir`** (pt-BR), renomeando junto `feature supersede` → `feature substituir` | `supersede` é estrangeirismo (memória `ptbr-formal-correctness`); é texto que o usuário digita/lê. Renomear o feature junto evita o mesmo conceito com duas palavras. v0.2, só o hub dogfoodando → *breaking change* trivial. Escopo escolhido pelo usuário: decision + feature (não todos os verbos). |
| **Camada de dado** | `superseded` (estado JSON), `superseded_by`, `domain.ActionSupersede`, evento `*.superseded`, `ref-to-superseded` **continuam inglês** | Regra *data-vs-UI* do CLAUDE.md: estados de ciclo são strings inglesas no dado; só prosa humana é pt-BR. |
| **Evento** | `decision.created` (criação) + `decision.superseded` (transição) | Realiza o `adr.superseded` previsto na 0001 sob o prefixo `decision.` já usado pelo importer (`decision.imported`). |
| **Orquestração** | Núcleo em `internal/app` (`CreateDecision` + `TransitionDecision`, par de `TransitionFeature`); CLI fino por cima | "CLI + server chamam o mesmo core" (CLAUDE.md). Reusa `CommitAndSnapshot` (commit+push+lock), `EventActor()` e `ValidateTransitionFlags`. |
| **Estado inicial** | Decision nasce `active` (sem `draft`/gate de conteúdo) | FSM da 0001 é `active/superseded` apenas. Consequência: um `decision new` recém-criado materializa o stub até o autor preenchê-lo (auto-corrige no próximo `pull`). Gate de conteúdo p/ decision = follow-up se incomodar. |
| **`import`** | **Inalterado** | Já é idempotente (dedup por `imported_from`); segue carregando o legado. ADR novo nasce nativo, então a autoria não depende dele. |

---

## Desenho

### `internal/app/decision.go` (novo)

Espelha `transition.go`/`historical.go`:

```go
// CreateDecision mints a new active decision in the backend clone: allocates the
// next D- id, saves the entity, writes a stub ADR doc, appends decision.created
// and commits+pushes the backend and refreshes the docs.lock snapshot.
func CreateDecision(repoDir, projDir, title string) (store.Decision, error)

// DecisionOutcome reports a decision transition that was applied and synced.
type DecisionOutcome struct {
    ID   string
    From domain.DecisionState
    To   domain.DecisionState
}

// TransitionDecision supersedes decision id by another decision in the backend
// clone: validates by/reason, loads both (errors when missing / self / already
// terminal), applies domain.TransitionDecision, persists state+superseded_by+
// reason, appends decision.superseded and commits+pushes+locks.
func TransitionDecision(repoDir, projDir, id, by, reason string) (DecisionOutcome, error)
```

- `CreateDecision`: `NextID("D")` → `store.Decision{State: domain.DecisionActive, CreatedAt/UpdatedAt: now, …}` → `SaveDecision` → stub em `content/decisions/<id>.md` (`---\ncerne_id: <id>\n---\n\n# <title>\n\n> Decisão pendente — descreva contexto, decisão e consequências.\n`) → `AppendEvent(decision.created, {title})` → `CommitAndSnapshot`.
- `TransitionDecision`: `LoadDecision(id)` (erro pt-BR se não existe); `ValidateTransitionFlags(domain.ActionSupersede, by, reason)`; checa `by != id` e `LoadDecision(by)` existe; `domain.TransitionDecision(d.State, domain.ActionSupersede)` (devolve `ErrInvalidTransition` se já `superseded`); seta `State/SupersededBy/Reason/UpdatedAt`; `SaveDecision`; `AppendEvent(decision.superseded, {by, reason})`; `CommitAndSnapshot`.

### `internal/cli/decisioncmd.go` (novo)

`newDecisionCmd()` (grupo `decision`, flag `--dir` oculta, como `feature`):
- `decision new <title>` → `app.CreateDecision`; saída `"%s criada (active). Próximo passo: escreva o ADR e rode 'cerne pull'."`
- `decision substituir <id> --by <id> --reason …` → `app.TransitionDecision`; saída `"%s: active → superseded"`.

Registrar em `root.go`: `root.AddCommand(newDecisionCmd())`.

### Renomeação `feature supersede` → `feature substituir`

Em `internal/cli/transitioncmd.go`, `transitionSpecs`: `{"substituir", "Substituir uma feature por outra (requer --by e --reason)", domain.ActionSupersede}`. As duas mensagens de `ValidateTransitionFlags` que dizem "para supersede" passam a "para a substituição" (pt-BR). `domain.ActionSupersede`, `eventTypes[ActionSupersede] = "feature.superseded"` e a mensagem de commit (que embute o *token* de ação, em inglês, igual para os 5 verbos) ficam como estão — dado/código.

---

## Verificação

- **Domain**: `decision_test.go` já cobre o FSM; ligar o caminho de produção não muda o FSM.
- **app** (novo): `CreateDecision` (entidade `active` + stub + evento + 1 commit), `TransitionDecision` (sucesso; erros: id inexistente, `--by` inexistente, `--by == id`, já `superseded`).
- **CLI**: `decision new` / `decision substituir` (saída pt-BR, flags obrigatórias); renomeação do `feature substituir` (comando responde no novo nome; ajuda/erros pt-BR).
- **Integração lint+materialize**: criar→substituir uma decision via CLI e conferir que ela some de `.docs/current/decisions/` e que uma referência a ela em doc vigente é flagrada por `ref-to-superseded`.
- **e2e**: estender o golden path com `decision new → (preenche ADR) → decision substituir`.
- `go vet` + suíte completa verdes.

---

## Fora de escopo

- **Import incremental** (dedup por `imported_from`) — **já entregue fora deste caso** (`importer.Unimported`); deixou de ser follow-up.
- **Gate de conteúdo / estado `draft`** para decision (hoje nasce `active`; stub materializa até o autor preencher).
- **Migração dos 45 ADRs legados** de `docs/architecture/adr/*.md` para autoria nativa — seguem como estão; só ADRs novos nascem nativos.
- **UI** (criar/substituir decision pela UI) — o núcleo (`internal/app`) fica pronto; endpoint + tela = follow-up.
- **Traduzir os demais verbos** (approve/reject/start/complete) — o escopo escolhido foi só `substituir`.
