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

import (
	"fmt"
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// EpicChild is the minimal shape StubForEpic needs from a child feature/task:
// id and title. Kept independent of internal/store so plantmpl carries no
// dependency on the entity model.
type EpicChild struct {
	ID    string
	Title string
}

// Sections are the canonical plan sections, in template order.
var Sections = doclang.Sections(doclang.Plan, doclang.PT)

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

<!-- METADADOS (preencha como linhas **Spec:** / **Branch:** / **Saída (Gauntlet):**):
Spec: link para a spec aprovada (../<id>/spec.md) — a fonte dos critérios de aceitação.
Branch: a branch/worktree onde o trabalho acontece.
Saída (Gauntlet): herde a barra da spec (repita a referência + acesso e a condição de saída — vencer/empatar,
derrota abre nova rodada, parar perdendo só por encerramento humano), OU declare em 1 linha por que o loop
não se aplica a esta entrega. Sem esta linha, a implementação começa sem saber contra o que será comparada. -->

## Estratégia
<!-- Em 2-3 frases: a abordagem e como o trabalho é fatiado (MR único ou N fases) e por quê. Não repita
a spec; aponte para ela. O leitor entende o "como", em alto nível, antes de descer aos detalhes.
ANTES de planejar: busque convenções registradas para este escopo — rode 'cerne note tags' (o menu de
tópicos) e puxe as notas relevantes com 'cerne note list --component <topico>'. Cite aqui os ids (N-NNNN)
das convenções aplicáveis — elas regem as tarefas abaixo. -->

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
revisão humana antes de prosseguir. Instruções literais — o agente segue ao pé da letra. Inclua as
convenções (notas N-NNNN citadas na Estratégia) que o agente deve obedecer ao executar. -->

## Definição de pronto
<!-- Checklist binário (cada item ATENDIDO/PENDENTE com evidência arquivo.go:linha — não "parece pronto"):
cada critério de aceitação da spec tem (a) implementação e (b) um teste que o prova, rastreado pelo número;
suíte completa verde; todas as tarefas concluídas; result.md escrito. É o portão de cerne feature complete. -->
`

// BodyEN is Body in English (F-0359).
const BodyEN = `<!-- HOW TO USE: this is the execution plan of a feature whose spec is already APPROVED. Do not repeat the spec —
reference it and describe HOW to build. Fill in every section and remove these guide lines. Mark any
uncertainty with [CLARIFY: specific question] and do not proceed by guessing. Size it by the work:
a trivial change (≤3 files) fits in a single task, and sections that do not apply become one line ("N/A")
— do not inflate a small plan with ceremony. Before cerne feature start: no [CLARIFY] left;
every task small, with a test and a verification tied to a spec criterion. -->

<!-- METADATA (fill in as lines **Spec:** / **Branch:** / **Exit (Gauntlet):**):
Spec: link to the approved spec (../<id>/spec.md) — the source of the acceptance criteria.
Branch: the branch/worktree where the work happens.
Exit (Gauntlet): inherit the bar from the spec (repeat the reference + access and the exit condition — win/tie,
a loss opens a new round, stopping while losing only by a recorded human decision), OR state in 1 line why the loop
does not apply to this delivery. Without this line, implementation starts without knowing what it will be compared against. -->

## Strategy
<!-- In 2-3 sentences: the approach and how the work is sliced (single MR or N phases) and why. Do not repeat
the spec; point to it. The reader understands the "how", at a high level, before the details.
BEFORE planning: look for conventions registered for this scope — run 'cerne note tags' (the topic
menu) and pull the relevant notes with 'cerne note list --component <topic>'. Cite here the ids (N-NNNN)
of the applicable conventions — they govern the tasks below. -->

## Task sequence
<!-- Small, ORDERED tasks; each leaves the repository green (build/tests passing). Per task:
- Files: create/edit/remove (real paths).
- Test first: the failing test (TDD); then the minimal implementation that makes it pass.
- Verification: the command that proves success (→ expected output), citing the number of the spec criterion it satisfies.
- Commit: the exact Conventional message.
Mark [parallel] when the task does not depend on the previous ones. -->

## Dependencies and order
<!-- What unblocks what (the real order, not just the list); tasks that can run in parallel; external dependencies
(other cases, services, migrations). If everything is sequential and simple, say so in one line. -->

## Risks and rollback
<!-- Per risk: how to detect + how to mitigate. Rollback: every committed task is a return point;
describe how to revert (git revert / undo a migration / etc.) and flag destructive or irreversible steps. -->

## Constraints for the agent
<!-- Execution guardrails: what NOT to touch; decisions already settled in the spec to respect; where to PAUSE for
human review before going on. Literal instructions — the agent follows them to the letter. Include the
conventions (notes N-NNNN cited in the Strategy) the agent must obey while executing. -->

## Definition of done
<!-- Binary checklist (each item MET/UNMET with file.go:line evidence — not "looks done"):
every acceptance criterion of the spec has (a) an implementation and (b) a test that proves it, tracked by number;
full suite green; all tasks finished; result.md written. It is the gate of cerne feature complete. -->
`

// BodyIn returns the template body in language l.
func BodyIn(l doclang.Lang) string {
	if l == doclang.EN {
		return BodyEN
	}
	return Body
}

// StubIn is Stub in language l.
func StubIn(l doclang.Lang, title string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + BodyIn(l)
}

// Stub returns the plan body cerne scaffolds when a feature enters the plan
// phase: the title heading followed by the template Body. Callers prepend the
// cerne_id frontmatter.
func Stub(title string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + Body
}

// executionMapGuide is the authoring instruction closing the scaffolded
// execution map, telling the author to group children into real parallel
// steps (cerne only knows the flat child list, not real dependency).
const executionMapGuide = "TODO: agrupe em etapas — mesma etapa = paralelas"

// StubForEpic returns the plan scaffold for a feature that already has
// children (an epic): identical to Stub, except the "Dependências e ordem"
// section gets a provisional Mapa de execução — one numbered step per child,
// in the canonical "Etapa N: ID (title)" format, wrapped in an HTML comment
// (like the rest of the template guidance) so it still reads as scaffold —
// the start gate stays closed until the author fills a real section (spec
// criterion 4). With no children it is byte-identical to Stub (criterion 2).
func StubForEpic(title string, children []EpicChild) string {
	return StubForEpicIn(doclang.PT, title, children)
}

// StubForEpicIn is StubForEpic in language l. The execution map keeps its
// canonical "Etapa N: ID (title)" lines in every language: it is a parsed
// format, not prose.
func StubForEpicIn(l doclang.Lang, title string, children []EpicChild) string {
	s := StubIn(l, title)
	if len(children) == 0 {
		return s
	}
	var m strings.Builder
	m.WriteString("<!-- Mapa de execução:\n")
	for i, c := range children {
		fmt.Fprintf(&m, "Etapa %d: %s (%s)\n", i+1, c.ID, c.Title)
	}
	m.WriteString(executionMapGuide + " -->")
	heading := "## " + doclang.Sections(doclang.Plan, l)[2] + "\n"
	return strings.Replace(s, heading, heading+"\n"+m.String()+"\n", 1)
}
