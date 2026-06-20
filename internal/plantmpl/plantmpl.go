// Package plantmpl holds the canonical PLAN template that cerne scaffolds when a
// feature is approved and asks the LLM to fill. It is the single source of truth
// for the plan structure, shared by the stub writer (internal/app) and the
// plan-draft prompt (internal/cli) so the two never drift. It is the execution
// counterpart of spectmpl: the spec says what to build, the plan says how.
//
// The section order is load-bearing, not cosmetic: Estratégia and the Sequência
// de tarefas sit at the top and Definição de pronto at the end, so the most
// decision-bearing, executable content and the exit gate land where long-context
// models attend best (primacy/recency) rather than buried in the middle. The plan
// must not duplicate the spec — context is a finite resource, so it carries only
// the high-signal "how" and links back to the spec for the "what". The rationale
// and its sources are registered in the cerne backend as references.
package plantmpl

import "strings"

// Sections are the canonical plan sections, in template order.
var Sections = []string{
	"Estratégia",
	"Sequência de tarefas",
	"Dependências e ordem",
	"Riscos e rollback",
	"Restrições para o agente",
	"Definição de pronto",
}

// Body is the plan template skeleton: the canonical section headings in order,
// each with inline authoring guidance as HTML comments. It carries no
// frontmatter and no title heading — callers prepend those. doccheck treats
// markdown headings and HTML-comment guidance as scaffold, so a freshly
// scaffolded plan still reads as a stub (the feature start gate stays closed)
// until a section is filled with real content. The [ESCLARECER: token is the
// same ambiguity marker the spec template uses (spectmpl.AmbiguityMarker) — one
// marker across the lifecycle, asserted by the tests.
const Body = `<!-- COMO USAR: este é o plano de execução de uma feature já com spec APROVADA. Não repita a spec —
referencie-a e descreva COMO construir. Preencha cada seção e remova estas linhas-guia. Marque qualquer
incerteza com [ESCLARECER: pergunta específica] e não prossiga adivinhando. Dimensione pelo trabalho:
mudança trivial (≤3 arquivos) cabe em uma única tarefa e seções que não se aplicam viram uma linha ("N/A")
— não infle um plano pequeno com cerimônia. Antes de cerne feature start: nenhum [ESCLARECER] restante;
cada tarefa pequena, com teste e verificação ligada a um critério da spec. -->

<!-- METADADOS (preencha como linhas **Spec:** / **Branch:** / **Status:**):
Spec: link para a spec aprovada (../<id>/spec.md) — a fonte dos critérios de aceitação.
Branch: a branch/worktree onde o trabalho acontece.
Status: Rascunho (AAAA-MM-DD) — plano para revisão. -->

## Estratégia
<!-- Em 2-3 frases: a abordagem e como o trabalho é fatiado (MR único ou N fases) e por quê. Não repita
a spec; aponte para ela. O leitor entende o "como", em alto nível, antes de descer aos detalhes. -->

## Sequência de tarefas
<!-- Tarefas pequenas e ORDENADAS; cada uma deixa o repositório verde (build/testes passando). Por tarefa:
- Arquivos: criar/editar/remover (caminhos reais).
- Teste primeiro: o teste que falha (TDD); depois a implementação mínima que o faz passar.
- Verificação: o comando que prova o sucesso (→ saída esperada), citando o número do critério da spec que satisfaz.
- Commit: a mensagem Conventional exata.
Marque [paralela] quando a tarefa não depende das anteriores. -->

## Dependências e ordem
<!-- O que destrava o quê (a ordem real, não só a lista); tarefas paralelizáveis; dependências externas
(outros casos, serviços, migrações). Se tudo é sequencial e simples, diga isso em uma linha. -->

## Riscos e rollback
<!-- Por risco: como detectar + como mitigar. Rollback: cada tarefa commitada é um ponto de retorno;
descreva como reverter (git revert / desfazer migração / etc.) e sinalize passos destrutivos ou irreversíveis. -->

## Restrições para o agente
<!-- Guardrails de execução: o que NÃO tocar; decisões já fechadas na spec a respeitar; onde PAUSAR para
revisão humana antes de prosseguir. Instruções literais — o agente segue ao pé da letra. -->

## Definição de pronto
<!-- Checklist binário (cada item ATENDIDO/PENDENTE com evidência arquivo.go:linha — não "parece pronto"):
cada critério de aceitação da spec tem (a) implementação e (b) um teste que o prova, rastreado pelo número;
suíte completa verde; todas as tarefas concluídas; result.md escrito. É o portão de cerne feature complete. -->
`

// Stub returns the plan body cerne scaffolds when a feature enters the plan
// phase: the title heading followed by the template Body. Callers prepend the
// cerne_id frontmatter.
func Stub(title string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + Body
}
