# Case 0039 — Template de result (resultado) provido pelo cerne

tags: #spec #cerne #cli #restmpl #docs

**Status:** Rascunho (2026-06-20) — spec para revisão.
**Tipo:** Novo recurso — completa a família de templates (spec F-0036, plano F-0037, ADR F-0038) com o resultado.
**Origem:** decisão do usuário (2026-06-20): "fazer o mesmo trabalho para os arquivos result.md também", embasada na análise de 254 result.md (hub + cerne) e em pesquisa de boas práticas.

## Problema

O resultado (result.md) é o único doc do ciclo sem template. F-0036/F-0037/F-0038 deram esqueleto canônico à spec, ao plano e ao ADR (`internal/spectmpl`, `internal/plantmpl`, `internal/dectmpl`), prompt de rascunho e scaffolding. O result.md, porém, é texto livre, autorado à mão antes de `cerne feature complete`, e não há `cerne result draft` nem scaffold.

Nos 254 result.md analisados (hub + cerne), o esqueleto recorrente é "o que entregou → verificação → desvios → próximos passos → inventário", mas com lacunas: contagem de testes ausente em ~30%, rastreabilidade dos critérios de aceitação em ~15–20%, e dívidas sem follow-up concreto em ~25%. O resultado é o ponto onde o loop deveria fechar — os critérios numerados da spec verificados com EVIDÊNCIA — e hoje cada autor reinventa isso.

## Objetivo e critérios de aceitação

Dar ao resultado o mesmo tratamento canônico do resto da família, fechando o loop spec → plano → result.

1. QUANDO uma feature inicia desenvolvimento (`cerne feature start`) O SISTEMA DEVE materializar `content/features/<id>/result.md` a partir do template canônico, e o scaffold DEVE ler como stub (`doccheck.HasRealContent` falso), mantendo o gate de `cerne feature complete` fechado.
2. O pacote `internal/restmpl` DEVE expor as seções canônicas em ordem — `Resultado`, `Aceitação`, `O que mudou`, `Débitos e follow-ups`, `Aprendizados e desvios` — e `Stub(title)`, com teste fixando a ordem.
3. QUANDO `cerne result draft <id>` roda O SISTEMA DEVE montar um prompt com a spec e o plano como contexto, pedir as 5 seções em ordem e as regras: registrar o que entregou, aceitação binária ATENDIDO/PENDENTE 1:1 com os critérios numerados e com EVIDÊNCIA, marcador `[ESCLARECER:`, right-sizing.
4. A skill `cerne-authoring` DEVE documentar a estrutura do result e a razão da ordenação (primacy/recency).
5. As fontes externas que embasaram o template DEVEM estar registradas como referências, com `--informs` apontando para esta feature.

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| Onde materializar | No `cerne feature start` (entrada da fase de implementação/entrega) | Espelha spec-no-`new`, plano-no-`approve`; o autor cai num closeout estruturado. |
| Aceitação | Binária ATENDIDO/PENDENTE, 1:1 com os critérios numerados, com evidência | Fecha o loop: spec define critérios, plano define pronto, result PROVA. Evita "parece pronto". |
| Marcador de ambiguidade | Reusar `spectmpl.AmbiguityMarker` | Um marcador em toda a família, sem drift. |
| Número de seções | 5 seções enxutas (não as 8 do levantamento bruto) | Right-sizing: o result do hub forte é conciso; mais seções viram cerimônia. |
| Ordenação | Resultado + Aceitação no topo; Aprendizados no fim | Primacy/recency: o desfecho e a prova onde a atenção é melhor. |

## Desenho

Espelha a família:

- `internal/restmpl/restmpl.go` — `Sections`, `Body` (lido como stub pelo `doccheck`), `Stub(title)`.
- `internal/cli/resultcmd.go` — `cerne result draft <id>` + `BuildResultDraftPrompt(f store.Feature, specContent, planContent, currentContent string) string` (injeta spec+plano como contexto). Registrado em `internal/cli/root.go`.
- `internal/app/feature.go` — `writeResultStub`. `internal/app/transition.go` — chama em `ActionStart`, antes do commit. Mensagem do `start` aponta o próximo passo.
- `internal/skills/cerne-authoring/SKILL.md` — seção "Result structure".

## Verificação

Plano de testes (TDD), 1:1 com os critérios:

- Crit. 1 → `internal/cli/transitioncmd_test.go:TestStartScaffoldsResultStub` + `internal/restmpl/restmpl_test.go:TestStubReadsAsStub`.
- Crit. 2 → `restmpl_test.go:TestSectionsCanonical` / `TestBodyHasSectionsInOrder`.
- Crit. 3 → `internal/cli/resultcmd_test.go:TestBuildResultDraftPrompt` (+ `TestResultDraftByoKeyWritesAndCommits`, `TestResultDraftUnknownFeature`).
- Crit. 4 → revisão da `SKILL.md`.
- Crit. 5 → `cerne reference new … --informs <id>`.

## Fora de escopo

- Regra de `cerne lint` para completude das seções do result (follow-up).
- Métricas/contagens automáticas de teste no result.
- Parsing de wiki-links no lint (gap pré-existente).
