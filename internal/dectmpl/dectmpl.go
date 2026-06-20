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

import "strings"

// Sections are the canonical ADR sections, in template order.
var Sections = []string{
	"Contexto",
	"Decisão",
	"Consequências",
	"Alternativas consideradas",
	"Implementação e relações",
}

// Body is the ADR template skeleton: the canonical section headings in order,
// each with inline authoring guidance as HTML comments. It carries no
// frontmatter and no title heading — callers prepend those. doccheck treats
// markdown headings and HTML-comment guidance as scaffold, so a freshly
// scaffolded ADR reads as a stub until a section is filled. The [ESCLARECER:
// token is the same ambiguity marker the spec and plan templates use
// (spectmpl.AmbiguityMarker) — one marker across the lifecycle, asserted by the
// tests.
const Body = `<!-- COMO USAR: este é o registro de uma decisão de arquitetura (ADR). Capture a decisão e suas forças e
consequências de forma DURÁVEL — não o passo a passo de implementação (isso é o plano). Preencha cada seção e
remova estas linhas-guia. Marque qualquer incerteza com [ESCLARECER: pergunta específica] e resolva antes de
tratar a decisão como aceita. Dimensione pela decisão: uma escolha pequena pode ter Alternativas e
Implementação em uma linha ("N/A") — não infle. O status (active/superseded) é dado gerido pelo cerne
(cerne decision substituir), não prosa. -->

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

// Stub returns the ADR body cerne scaffolds for a new decision: the title
// heading followed by the template Body. Callers prepend the cerne_id
// frontmatter.
func Stub(title string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + Body
}
