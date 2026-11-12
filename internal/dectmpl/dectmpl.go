// Package dectmpl holds the canonical ADR (decision) template that cerne
// scaffolds when a decision is created and asks the LLM to fill. It is the
// single source of truth for the decision-record structure, shared by the stub
// writer (internal/app) and the decision-draft prompt (internal/cli) so the two
// never drift — the third member of the spectmpl/plantmpl family.
//
// The shape follows Michael Nygard's classic ADR spine (Contexto → Decisão →
// Consequências), extended with the Alternativas the hub corpus relies on and an
// Implementação e relações section that anchors the decision in real code and
// links the features/ADRs it touches. The section order is load-bearing:
// Contexto and Decisão sit at the top (the most decision-bearing content, where
// long-context models attend best — primacy), and Implementação e relações last
// (lookup material — recency is fine). Lifecycle status (active/superseded) is
// cerne data managed by `cerne decision substituir`, not prose, so the template
// carries no Status section. The rationale and its sources are registered in the
// cerne backend as references.
package dectmpl

import (
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// Sections are the canonical ADR sections, in template order.
var Sections = doclang.Sections(doclang.Decision, doclang.PT)

// Body is the ADR template skeleton: the canonical section headings in order,
// each with inline authoring guidance as HTML comments. It carries no
// frontmatter and no title heading — callers prepend those. doccheck treats
// markdown headings and HTML-comment guidance as scaffold, so a freshly
// scaffolded ADR reads as a stub until a section is filled. The [ESCLARECER:
// token is the same ambiguity marker the spec and plan templates use
// (spectmpl.AmbiguityMarker) — one marker across the lifecycle, asserted by the
// tests.
const Body = `<!-- COMO USAR: este é o registro de uma decisão de arquitetura JÁ TOMADA (ADR), não uma proposta em aberto.
Capture a decisão e suas forças e consequências de forma DURÁVEL e CONCISA — não o passo a passo de
implementação (isso é o plano). O título registra a DECISÃO, não a pergunta (ex.: "Usar Postgres como storage
primário", não "Qual banco usar?"). Preencha cada seção e remova estas linhas-guia. Marque qualquer incerteza
com [ESCLARECER: pergunta específica] e resolva antes de tratar a decisão como aceita. Dimensione pela decisão:
uma escolha pequena pode ter Alternativas e Implementação em uma linha ("N/A") — não infle. Um ADR aceito é
registro histórico: para mudar de rumo, supersede (cerne decision substituir), não reescreva. O status
(active/superseded) é dado gerido pelo cerne, não prosa. -->

<!-- METADADOS (preencha como linha **Data:**):
Data: AAAA-MM-DD — quando a decisão foi tomada. -->

## Contexto
<!-- As forças e restrições em tensão e o gatilho (por que decidir agora). Ancore no real (código, caso,
requisito). Nomeie as forças — não deixe o leitor inferir. Só o contexto — a escolha vem depois. -->

## Decisão
<!-- A escolha, no imperativo ("Vamos X" / "Usaremos X"), com as 2-3 razões que a fizeram vencer.
Inequívoca: o agente segue ao pé da letra. -->

## Consequências
<!-- O que decorre da decisão: positivas, NEGATIVAS/trade-offs (sempre liste-as) e restrições/invariantes
(o que NÃO pode mudar sem revisitar este ADR). O leitor futuro precisa saber o que observar. -->

## Alternativas consideradas
<!-- As opções avaliadas (2+ quando há trade-off real), cada uma com prós/contras e POR QUE foi rejeitada.
Não dispense em uma linha algo que pesou de verdade na decisão. -->

## Implementação e relações
<!-- Onde a decisão aparece no código (arquivos/módulos reais) e os vínculos: features e ADRs que ela afeta,
supersede ou que a motivaram — use os ids (F-/D-). -->
`

// BodyEN is Body in English (F-0359).
const BodyEN = `<!-- HOW TO USE: this is the record of an architecture decision ALREADY MADE (ADR), not an open proposal.
Capture the decision and its forces and consequences in a DURABLE and CONCISE way — not the step-by-step of
the implementation (that is the plan). The title records the DECISION, not the question (e.g. "Use Postgres as the
primary store", not "Which database?"). Fill in every section and remove these guide lines. Mark any uncertainty
with [CLARIFY: specific question] and resolve it before treating the decision as accepted. Size it by the decision:
a small choice can have Alternatives and Implementation in one line ("N/A") — do not inflate. An accepted ADR is a
historical record: to change course, supersede it (cerne decision substituir), do not rewrite it. The status
(active/superseded) is data managed by cerne, not prose. -->

<!-- METADATA (fill in as a line **Date:**):
Date: YYYY-MM-DD — when the decision was made. -->

## Context
<!-- The forces and constraints in tension and the trigger (why decide now). Anchor it in what is real (code, case,
requirement). Name the forces — do not leave the reader to infer them. The context only — the choice comes later. -->

## Decision
<!-- The choice, in the imperative ("We will X" / "We will use X"), with the 2-3 reasons that made it win.
Unambiguous: the agent follows it to the letter. -->

## Consequences
<!-- What follows from the decision: positive, NEGATIVE/trade-offs (always list them) and constraints/invariants
(what must NOT change without revisiting this ADR). The future reader needs to know what to watch. -->

## Alternatives considered
<!-- The options weighed (2+ when there is a real trade-off), each with pros/cons and WHY it was rejected.
Do not dismiss in one line something that truly weighed on the decision. -->

## Implementation and relations
<!-- Where the decision shows up in the code (real files/modules) and the links: features and ADRs it affects,
supersedes or that motivated it — use the ids (F-/D-). -->
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

// Stub returns the ADR body cerne scaffolds for a new decision: the title
// heading followed by the template Body. Callers prepend the cerne_id
// frontmatter.
func Stub(title string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + Body
}
