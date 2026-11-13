# Documentation language

## Documentation language (F-0359)

A project documents in **one** language, recorded in its backend (`project.json` `docs_language`): `en` or `pt-BR`. `cerne init` writes `en` for a new backend; `cerne init --docs-language pt-BR` picks Portuguese; a backend without the field is `pt-BR`. A second `init` that joins an existing backend inherits its language. The scaffolds cerne writes (spec, plan, result, decision, runbook) and the `draft` prompts follow it. **Write every document in the project's language**: check the headings of the stub you are filling.

Reading is bilingual in every project: lint and the gates accept either vocabulary, and a document may mix them. The sections and literal forms below are shown in Portuguese throughout this guide; each has an exact English form:

| Document | Portuguese sections | English sections |
|---|---|---|
| Spec | Problema · Objetivo e critérios de aceitação · Decisões do design · Desenho · Verificação · Execução recomendada (agente) · Fora de escopo | Problem · Goal and acceptance criteria · Design decisions · Design · Verification · Recommended execution (agent) · Out of scope |
| Plan | Estratégia · Sequência de tarefas · Dependências e ordem · Riscos e rollback · Restrições para o agente · Definição de pronto | Strategy · Task sequence · Dependencies and order · Risks and rollback · Constraints for the agent · Definition of done |
| Result | Resultado · Aceitação · O que mudou · Débitos e follow-ups · Aprendizados e desvios · Gauntlet | Result · Acceptance · What changed · Debts and follow-ups · Learnings and deviations · Gauntlet |
| Decision | Contexto · Decisão · Consequências · Alternativas consideradas · Implementação e relações | Context · Decision · Consequences · Alternatives considered · Implementation and relations |
| Runbook | Quando usar · Pré-requisitos · Procedimento · Verificação · Rollback e troubleshooting | When to use · Prerequisites · Procedure · Verification · Rollback and troubleshooting |

| Literal form | Portuguese | English |
|---|---|---|
| Ambiguity marker | `[ESCLARECER: …]` / `[ESCLARECIDO: …]` | `[CLARIFY: …]` / `[CLARIFIED: …]` |
| EARS criterion | `QUANDO <condição> O SISTEMA DEVE <comportamento>` | `WHEN <condition> THE SYSTEM SHALL <behaviour>` |
| Bar | `Barra: <referência + acesso>` | `Bar: <named reference + access>` |
| Method and exit lines | `Método:` / `Saída:` | `Method:` / `Exit:` |
| No applicable bar | `Barra: não aplicável (<justificativa>) — padrão alternativo: <…>` | `Bar: not applicable (<justification>) — alternative standard: <…>` |
| Waived by a human | `Barra: dispensada por decisão humana (<citação>)` | `Bar: waived by human decision (<quote>)` |
| Evidence citation | `critério N`, `critérios N-M` | `criterion N`, `criteria N-M` |
| Gauntlet round | `Rodada N — <vitória\|derrota\|empate>: "<citação>"` | `Round N — <win\|loss\|tie>: "<quote>"` |
| Critic findings | `Achados do crítico: <n> — …` | `Critic findings: <n> — …` |
| Judge's fixed last line | `VEREDITO: A` / `B` / `EMPATE` | `VERDICT: A` / `B` / `TIE` |

Not translated yet, in any project: the `## Custo` and `## Entrega` stamps cerne writes into a result, an epic plan's execution map (`Etapa N: ID (título)`), the `**Origem:**` line cerne writes when it promotes a pending or an idea, and the compiled wiki, `cerne ask` and the Backlog analyst. Leave those as cerne writes them.
