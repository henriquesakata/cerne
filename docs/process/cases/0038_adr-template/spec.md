# Case 0038 — Template de ADR (decisão) provido pelo cerne

tags: #spec #cerne #cli #dectmpl #docs

**Status:** Rascunho (2026-06-20) — spec para revisão.
**Tipo:** Novo recurso — estende o trabalho de template de spec (F-0036) e de plano (F-0037) para a fase de decisão (ADR).
**Origem:** decisão do usuário (2026-06-20): "fazer o mesmo trabalho com a escrita de ADR", embasada na análise dos 46 ADRs do hub e em pesquisa de boas práticas (Nygard, MADR, ThoughtWorks).

## Problema

A decisão (ADR) não tem template. F-0036/F-0037 deram esqueleto canônico à spec e ao plano (`internal/spectmpl`, `internal/plantmpl`), prompt de rascunho (`spec draft`, `plan draft`), scaffolding e documentação. O ADR, porém, nasce de um stub de uma linha em `internal/app/decision.go` (`"> Decisão pendente — descreva contexto, decisão e consequências."`) e não há `cerne decision draft`.

Nos 46 ADRs do hub (`hub/docs/architecture/adr/`), o formato é Nygard (Contexto → Decisão → Consequências) + Alternativas (MADR) + extensões, mas com lacunas recorrentes: consequências **negativas** ausentes em ~20%, alternativas dispensadas em uma linha, status/data inconsistentes e sem backlinks de supersessão. Além disso, `internal/doccheck` não lista `"> Decisão pendente"` em `stubPrefixes` — um stub de decisão antigo conta como conteúdo real (inerte hoje, pois decisão não tem gate, mas incorreto). Sem template, cada ADR reinventa a estrutura e perde trade-offs.

## Objetivo e critérios de aceitação

Dar ao ADR o mesmo tratamento canônico da spec e do plano, embasado em Nygard + MADR + ThoughtWorks e nas lacunas do hub.

1. QUANDO `cerne decision new` cria uma decisão O SISTEMA DEVE materializar `content/decisions/D-NNNN.md` a partir do template canônico, e o scaffold DEVE ler como stub (`doccheck.HasRealContent` falso).
2. O pacote `internal/dectmpl` DEVE expor as seções canônicas em ordem — `Contexto`, `Decisão`, `Consequências`, `Alternativas consideradas`, `Implementação e relações` — e `Stub(title)`, com teste fixando a ordem.
3. QUANDO `cerne decision draft <id>` roda O SISTEMA DEVE montar um prompt pedindo as 5 seções em ordem e as regras: decisão no imperativo, consequências **negativas** sempre listadas, alternativas com o porquê da rejeição, marcador `[ESCLARECER:`, right-sizing.
4. `internal/doccheck` DEVE tratar `"> Decisão pendente"` como stub (corrige a lacuna em `stubPrefixes`).
5. A skill `cerne-authoring` DEVE documentar a estrutura do ADR e a razão da ordenação (primacy/recency); o estado de ciclo de vida (ativo/substituído) é dado gerido pelo cerne, não prosa.
6. As fontes externas que embasaram o template DEVEM estar registradas como referências, com `--informs` apontando para esta feature.

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| Estrutura base | Nygard (Contexto → Decisão → Consequências) + Alternativas + Implementação e relações | Nygard é o padrão de fato; MADR/hub agregam alternativas e âncoras de código que tornam o registro durável. |
| Estado no ADR | Sem seção de status em prosa | O ciclo de vida é dado gerido pelo cerne (`decision substituir`); duplicar em prosa desincronizaria. |
| Consequências | Negativas/trade-offs **obrigatórias** | Lacuna do hub (~20% sem negativas); é o que o leitor futuro mais precisa. |
| Marcador de ambiguidade | Reusar `spectmpl.AmbiguityMarker` | Um marcador em todo o ciclo (spec/plano/ADR), sem drift. |
| Scaffold | No `cerne decision new` | Espelha spec-no-`new`/plano-no-`approve`; a decisão nasce com estrutura. |
| Ordenação | Contexto+Decisão no topo; Implementação e relações no fim | Primacy/recency: a decisão e suas forças onde a atenção é melhor; relações são material de consulta. |

## Desenho

Espelha F-0036/F-0037:

- `internal/dectmpl/dectmpl.go` — `Sections`, `Body` (lido como stub pelo `doccheck`), `Stub(title)`.
- `internal/cli/decisioncmd.go` — `cerne decision draft <id>` + `BuildDecisionDraftPrompt(d store.Decision, currentContent string) string` (delegação imprime; byo-key escreve+push). Mensagem do `decision new` aponta o draft.
- `internal/app/decision.go` — `writeDecisionStub` passa a usar `dectmpl.Stub`.
- `internal/doccheck/doccheck.go` — `stubPrefixes` ganha `"> Decisão pendente"`.
- `internal/skills/cerne-authoring/SKILL.md` — seção "ADR structure".

## Verificação

Plano de testes (TDD), 1:1 com os critérios:

- Crit. 1 → `internal/cli/decisioncmd_test.go:TestDecisionNew` (scaffold com seções) + `internal/dectmpl/dectmpl_test.go:TestStubReadsAsStub`.
- Crit. 2 → `dectmpl_test.go:TestSectionsCanonical` / `TestBodyHasSectionsInOrder`.
- Crit. 3 → `decisioncmd_test.go:TestBuildDecisionDraftPrompt` (+ `TestDecisionDraftByoKeyWritesAndCommits`, `TestDecisionDraftUnknownDecision`).
- Crit. 4 → `dectmpl_test.go:TestStubReadsAsStub` cobre o caminho do template; o stub legado entra em `stubPrefixes`.
- Crit. 5 → revisão da `SKILL.md`.
- Crit. 6 → `cerne reference new … --informs <id>`.

## Fora de escopo

- Regra de `cerne lint` para completude das seções do ADR (follow-up).
- eADR / categorias do MADR para projetos grandes.
- Parsing de wiki-links no lint (gap pré-existente).
