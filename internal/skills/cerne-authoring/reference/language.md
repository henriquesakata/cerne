## Documentation language (F-0359)

A project documents in **one** language, recorded in its backend (`project.json` `docs_language`): `en` or `pt-BR`. `cerne init` writes `en` for a new backend; `cerne init --docs-language pt-BR` picks Portuguese; a backend without the field is `pt-BR`. A second `init` that joins an existing backend inherits its language. The scaffolds cerne writes (spec, plan, result, decision, runbook) and the `draft` prompts follow it. **Write every document in the project's language**: the session-start context carries a DOCS LANGUAGE line naming it and listing its forms; without it, check the headings of the stub you are filling.

Reading is bilingual in every project: lint and the gates accept either vocabulary, and a document may mix them. This guide names each section and form in English first, with the pt-BR form beside it; use only the forms of your project's language:

| Document | English sections | Portuguese sections |
|---|---|---|
| Spec | Problem · Goal and acceptance criteria · Design decisions · Design · Verification · Recommended execution (agent) · Out of scope | Problema · Objetivo e critérios de aceitação · Decisões do design · Desenho · Verificação · Execução recomendada (agente) · Fora de escopo |
| Plan | Strategy · Task sequence · Dependencies and order · Risks and rollback · Constraints for the agent · Definition of done | Estratégia · Sequência de tarefas · Dependências e ordem · Riscos e rollback · Restrições para o agente · Definição de pronto |
| Result | Result · Acceptance · What changed · Debts and follow-ups · Learnings and deviations · Gauntlet | Resultado · Aceitação · O que mudou · Débitos e follow-ups · Aprendizados e desvios · Gauntlet |
| Decision | Context · Decision · Consequences · Alternatives considered · Implementation and relations | Contexto · Decisão · Consequências · Alternativas consideradas · Implementação e relações |
| Runbook | When to use · Prerequisites · Procedure · Verification · Rollback and troubleshooting | Quando usar · Pré-requisitos · Procedimento · Verificação · Rollback e troubleshooting |

| Literal form | English | Portuguese |
|---|---|---|
| Ambiguity marker | `[CLARIFY: …]` / `[CLARIFIED: …]` | `[ESCLARECER: …]` / `[ESCLARECIDO: …]` |
| EARS criterion | `WHEN <condition> THE SYSTEM SHALL <behaviour>` | `QUANDO <condição> O SISTEMA DEVE <comportamento>` |
| Bar | `Bar: <named reference + access>` | `Barra: <referência + acesso>` |
| Method and exit lines | `Method:` / `Exit:` | `Método:` / `Saída:` |
| No applicable bar | `Bar: not applicable (<justification>) — alternative standard: <…>` | `Barra: não aplicável (<justificativa>) — padrão alternativo: <…>` |
| Waived by a human | `Bar: waived by human decision (<quote>)` | `Barra: dispensada por decisão humana (<citação>)` |
| Evidence citation | `criterion N`, `criteria N-M` | `critério N`, `critérios N-M` |
| Gauntlet round | `Round N — <win\|loss\|tie>: "<quote>"` | `Rodada N — <vitória\|derrota\|empate>: "<citação>"` |
| Critic findings | `Critic findings: <n> — …` | `Achados do crítico: <n> — …` |
| Judge's fixed last line | `VERDICT: A` / `B` / `TIE` | `VEREDITO: A` / `B` / `EMPATE` |

Not translated yet, in any project: the `## Custo` and `## Entrega` stamps cerne writes into a result, an epic plan's execution map (`Etapa N: ID (título)`), the `**Origem:**` line cerne writes when it promotes a pending or an idea, and the compiled wiki, `cerne ask` and the Backlog analyst. Leave those as cerne writes them.
