# Case 0037 — Template de plano provido pelo cerne para novos planos

tags: #spec #cerne #cli #plantmpl #docs

**Status:** Rascunho (2026-06-20) — spec para revisão.
**Tipo:** Novo recurso — estende o trabalho de template de spec (F-0036) para a fase de plano.
**Origem:** decisão do usuário (2026-06-20): "aplicar o mesmo trabalho da F-0036 para padronizar a escrita do plano", embasada em análise dos 125 planos do hub e em pesquisa de boas práticas de desenvolvimento com agentes de IA.

## Problema

A fase de **plano** do ciclo de feature não tem template. F-0036 deu à spec um esqueleto canônico (`internal/spectmpl`), um prompt de rascunho (`cerne spec draft`), scaffolding no `cerne feature new` e documentação na skill; o `plan.md`, porém, segue **texto livre** — `internal/app/feature.go` só faz `writeSpecStub` na criação, e `internal/app/transition.go:150` apenas verifica se existe um `plan.md` real no gate de `cerne feature start`, sem oferecer estrutura.

O resultado, medido nos 125 planos do hub (`hub/docs/process/cases/*/plan.md`): variância enorme (de 16 linhas a 3.000) e lacunas recorrentes — rollback presente em 7%, critérios de aceitação espelhados em 10%, bloqueios/itens em aberto em 2%, e ausência de DAG de dependências, marcador de ambiguidade e guardrails para o agente. A metade executável (caminhos reais, TDD, commits exatos) já é boa quando presente; falta padronizar o resto. Sem template, cada plano reinventa a estrutura e o agente executor recebe contexto desigual.

## Objetivo e critérios de aceitação

Dar ao plano o mesmo tratamento canônico da spec, embasado nas melhores práticas de mercado, para que humanos e agentes produzam planos densos, executáveis e verificáveis.

1. QUANDO uma feature é aprovada (`cerne feature approve`) O SISTEMA DEVE materializar `content/features/<id>/plan.md` a partir do template canônico; o scaffold DEVE continuar contando como stub (`doccheck.HasRealContent` falso), mantendo o gate de `cerne feature start` fechado até o plano ser preenchido.
2. O pacote `internal/plantmpl` DEVE expor as seções canônicas em ordem — `Estratégia`, `Sequência de tarefas`, `Dependências e ordem`, `Riscos e rollback`, `Restrições para o agente`, `Definição de pronto` — e `Stub(title)`, com teste fixando a ordem (asserção direta).
3. QUANDO o autor roda `cerne plan draft <id>` O SISTEMA DEVE imprimir/gerar um prompt que inclui a spec aprovada como contexto, pede as seções do plano em ordem e ensina as regras: tarefa pequena teste-primeiro, verificação citando o número do critério da spec, marcador `[ESCLARECER:`, right-sizing e definição de pronto binária.
4. A skill `cerne-authoring` DEVE documentar a estrutura do plano e a razão da ordenação (primacy/recency), espelhando a seção de spec.
5. As fontes externas que embasaram o template DEVEM estar registradas como referências no backend, com `--informs` apontando para esta feature.

## Decisões do design

| Fork | Decisão | Razão |
|---|---|---|
| Onde materializar o plano | No `cerne feature approve` (entrada da fase de plano) | Espelha o stub de spec no `feature new`; o autor cai num plano estruturado, não num arquivo vazio. Alternativa rejeitada: só via `plan draft`. |
| Marcador de ambiguidade | Reusar `spectmpl.AmbiguityMarker` (`[ESCLARECER:`) | Um marcador em todo o ciclo; sem drift (teste cruza os dois pacotes). |
| Número de seções | 6 seções fixas, sem adicionar seções por tipo de trabalho | Right-sizing (tlc-spec-driven): complexidade define a profundidade; seções colapsam para "N/A". Adicionar seções seria a cerimônia que a fonte alerta evitar. |
| Definição de pronto | Checklist **binário** (ATENDIDO/PENDENTE) com evidência `arquivo:linha`, impl e teste separados | spec-driven-eval: checagem binária é reprodutível; impl e testes falham de forma independente. |
| Ordenação | `Estratégia`+`Sequência de tarefas` no topo, `Definição de pronto` no fim | Context engineering / lost-in-the-middle: conteúdo executável e o gate de saída onde a atenção do modelo é melhor (primacy/recency). |

## Desenho

Espelha F-0036, arquivo por arquivo:

- `internal/plantmpl/plantmpl.go` — `Sections []string`, `Body` (esqueleto com guia em comentários HTML, lido como stub pelo `doccheck`), `Stub(title string) string`. Doc de pacote registra a razão da ordenação.
- `internal/cli/plancmd.go` — `newPlanCmd`/`newPlanDraftCmd` (`cerne plan draft <id>`), `BuildPlanDraftPrompt(f store.Feature, specContent, currentContent string) string`, `writePlanDraft`, `readFeatureDoc`. Modo delegação imprime o prompt; byo-key/claude-cli gera, escreve e dá push. Registrado em `internal/cli/root.go`.
- `internal/app/feature.go` — `writePlanStub(repoDir, id, title string) error` (no-op se já existe plano real). `internal/app/transition.go` — chama `writePlanStub` em `ActionApprove`, antes do commit.
- `internal/cli/transitioncmd.go` — mensagem pós-approve aponta o próximo passo (preencher o plano).
- `internal/skills/cerne-authoring/SKILL.md` — seção "Plan structure" + passo Plan do loop atualizado.

## Verificação

Plano de testes (TDD), 1:1 com os critérios:

- Crit. 1 → `internal/cli/transitioncmd_test.go:TestApproveScaffoldsPlanStub` (approve materializa o plano com as seções, e `start` segue barrado por plan.md) + `internal/plantmpl/plantmpl_test.go:TestStubReadsAsStub`.
- Crit. 2 → `plantmpl_test.go:TestSectionsCanonical` / `TestBodyHasSectionsInOrder`.
- Crit. 3 → `internal/cli/plancmd_test.go:TestBuildPlanDraftPrompt` (+ `TestPlanDraftByoKeyWritesAndCommits`, `TestPlanDraftUnknownFeature`).
- Crit. 4 → revisão da `SKILL.md` (seção presente, ordem documentada).
- Crit. 5 → `cerne reference new … --informs <id>` para as 7 fontes; `cerne ask` as cita.

## Fora de escopo

- Regra de `cerne lint` para completude das seções do plano (follow-up).
- Parsing de wiki-links no lint (gap conhecido, não introduzido aqui).
- `cerne import` carregar `parent` (gap pré-existente).
