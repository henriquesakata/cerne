# Case 0036 — Lint `stale-candidate` (guard do registro-no-início)

tags: #spec #cerne #lint

**Status:** Rascunho (2026-06-20) — spec para revisão; `plan.md`/`result.md` a seguir.
**Tipo:** Nova regra de lint determinística em `internal/lint`. Aditiva.
**Origem:** Guard do caso 0035: registrar a task no início enche a coluna CANDIDATAS; sem varredura, candidatas abandonadas (registradas e nunca elaboradas) apodrecem no board. É o mecanismo anti-poluição do par "registre cedo".

---

## Problema

A regra do caso 0035 manda registrar a task como `candidate` no início. Isso é o certo para visibilidade, mas abre um flanco: uma candidate criada e depois abandonada (sem spec, sem `reject`) fica no board para sempre. O lint atual **não** cobre `candidate` — `gateFindings` (regra `missing-gate-doc`, em [lint.go](../../../../internal/lint/lint.go)) só checa `approved` em diante e faz `default: continue` para candidate. Logo, candidata-sem-spec não é vista por regra nenhuma. Sem um guard, o medo de poluição do usuário se concretiza.

## Objetivo e critérios de aceitação

Objetivo: sinalizar candidatas paradas sem spec, sem incomodar trabalho fresco nem em progresso.

1. QUANDO uma feature está em `candidate` há mais de `staleCandidateDays` (30) E não tem `spec.md` real, O SISTEMA DEVE emitir um finding `stale-candidate` para ela.
2. QUANDO uma candidate está dentro da janela (idade < 30 dias), O SISTEMA NÃO DEVE sinalizá-la (graça do registro-no-início).
3. QUANDO uma candidate já tem `spec.md` real, O SISTEMA NÃO DEVE sinalizá-la (está progredindo; basta `approve`).
4. QUANDO a feature não está em `candidate`, a regra a ignora (outras regras cuidam).
5. A regra é determinística: `now` é injetável para teste; `Run` passa `time.Now()`.
6. O finding entra em `RuleOrder`/`ruleRank` (ordem estável), no CLI (`cerne lint`, `--fix-tasks`) e no label da UI.

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| Relógio da idade | `f.CreatedAt` | Candidate só sai via `approve` (que exige spec); enquanto candidate, `CreatedAt` ≈ entrada no estado. Simples e correto. |
| Limiar | `const staleCandidateDays = 30` | Segue o padrão de consts do pacote (ex.: `minTitleNeedleLen`); não há sistema de config de lint ainda. Tunável depois. |
| Só sem-spec | Candidate com spec real fica silenciosa | Tem spec = progredindo; flag seria ruído. O sinal mais limpo de poluição é "registrada e nunca elaborada". |
| Injeção de `now` | Parâmetro na função pura; `Run` passa `time.Now()` | Mantém o resto das regras determinístico e o teste exato (sem relógio de parede). |

## Desenho

### [internal/lint/lint.go](../../../../internal/lint/lint.go)
- `const RuleStaleCandidate = "stale-candidate"`; entra em `RuleOrder` (pos 8) e `ruleRank` (8).
- `const staleCandidateDays = 30`.
- `func staleCandidateFindings(repoDir string, features []store.Feature, now time.Time) ([]Finding, error)`: para cada `candidate`, se `now.Sub(f.CreatedAt) >= 30d` e `isRealFile(.../spec.md)` é falso, emite `Finding{Rule: RuleStaleCandidate, Entity: f.ID, Detail: "candidata há N dias sem spec — rejeite ou avance com uma spec"}`. Reusa o helper `isRealFile`.
- `Run` chama `staleCandidateFindings(repoDir, features, time.Now())` antes do sort.

### [internal/cli/lintcmd.go](../../../../internal/cli/lintcmd.go)
- `taskAction`: branch `RuleStaleCandidate` → "rejeitar a candidata (cerne feature reject) ou escrever a spec e aprovar".

### [ui/src/Lint.tsx](../../../../ui/src/Lint.tsx)
- `RULE_LABELS['stale-candidate'] = 'Candidata parada (sem spec)'` + entrada em `RULE_ORDER`.

## Verificação (TDD)

- `TestStaleCandidateFindings` (função pura, `now` fixo): stale-sem-spec → 1 finding com detalhe exato; fresh → silêncio; stale-com-spec → silêncio; não-candidate → silêncio.
- `TestRunFlagsStaleCandidate`: candidate ancião (`CreatedAt` −365d) sem spec via `store.SaveFeature` → `Run` retorna exatamente 1 finding `stale-candidate` para ela (prova o fio em `Run`; o detalhe varia com o relógio, então só `Rule`/`Entity` são asseridos).
- Regressão: `TestRunFindings`/`TestLintFindings` seedam `CreatedAt=now` → a regra fica silenciosa → suítes existentes seguem verdes sem edição (aditivo).

## Fora de escopo

- **Candidate com spec real mas não aprovada** (outro tipo de staleness) — fora; só sem-spec aqui.
- **Limiar configurável** via `.cernerc` — const por ora.
- **Os 3 rules de épico ausentes na UI** (`parent-not-found`/`parent-terminal`/`epic-open-children`) — gap pré-existente do `Lint.tsx`, não deste caso (flag separada).
- **Rebuild do `ui/dist`** — o source da UI mudou; o dist recompila num passo limpo no ambiente canônico (rebuild aqui re-hasheia ~200 chunks de vendor).
