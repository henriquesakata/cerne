// Package runtmpl holds the canonical RUNBOOK template that cerne scaffolds when
// a runbook is created and asks the LLM to fill. It is the single source of truth
// for the runbook structure, shared by the stub writer (internal/app) and the
// runbook-draft prompt (internal/cli) so the two never drift — the operational
// member of the spectmpl/plantmpl/dectmpl/restmpl family.
//
// A runbook is an actionable operational procedure (deploy, smoke, recovery,
// migration, setup), read mid-incident. The section order is load-bearing:
// Quando usar, Pré-requisitos and Procedimento sit at the top, so the trigger and
// the steps land where long-context models (and stressed operators) attend best
// (primacy), and Rollback e troubleshooting last (recency is fine). Every step is
// a command with its expected output, never a paragraph. The rationale and its
// sources are registered in the cerne backend as references.
package runtmpl

import (
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// Sections are the canonical runbook sections, in template order.
var Sections = doclang.Sections(doclang.Runbook, doclang.PT)

// Body is the runbook template skeleton: the canonical section headings in order,
// each with inline authoring guidance as HTML comments. It carries no
// frontmatter and no title heading — callers prepend those. doccheck treats
// markdown headings and HTML-comment guidance as scaffold, so a freshly
// scaffolded runbook reads as a stub until a section is filled. The [ESCLARECER:
// token is the same ambiguity marker the rest of the family uses
// (spectmpl.AmbiguityMarker), asserted by the tests.
const Body = `<!-- COMO USAR: este é um runbook — um procedimento operacional ACIONÁVEL (deploy, smoke, recuperação,
migração, setup). Cada passo é um comando, não um parágrafo; mostre a SAÍDA ESPERADA ("você deve ver: X").
Preencha cada seção e remova estas linhas-guia. Marque qualquer incerteza com [ESCLARECER: pergunta específica].
Dimensione pela tarefa: runbook simples tem seções curtas — não infle. O status (active/superseded) é dado
gerido pelo cerne (cerne runbook substituir), não prosa. -->

<!-- METADADOS (preencha como linhas **Data:** / **Dono:**):
Data: AAAA-MM-DD — última revisão/validação.
Dono: time ou pessoa responsável (a quem escalar). -->

## Quando usar
<!-- A situação que dispara este runbook: gatilho, sintomas ou objetivo. O leitor chega no meio de um incidente
e precisa confirmar em segundos que é o runbook certo. -->

## Pré-requisitos
<!-- O que ter antes de começar: acessos/tokens, ferramentas e versões, estado do ambiente, janela/aprovações.
Para o operador confirmar que está pronto. -->

## Procedimento
<!-- Passos NUMERADOS, ação primeiro, comandos copiáveis. Após cada passo, a saída esperada ("você deve ver: X")
para o operador reconhecer sucesso. Anote idempotência quando um passo puder ser repetido com segurança. -->

## Verificação
<!-- Como confirmar que deu certo: checagens/sinais observáveis, com a saída esperada. O estado final, sem ambiguidade. -->

## Rollback e troubleshooting
<!-- Como desfazer/recuperar se algo falhar (sinalize passos destrutivos). Falhas comuns como tabela
sintoma → causa → solução. E a quem escalar quando o runbook não resolve. -->
`

// BodyEN is Body in English (F-0359).
const BodyEN = `<!-- HOW TO USE: this is a runbook — an ACTIONABLE operational procedure (deploy, smoke, recovery,
migration, setup). Every step is a command, not a paragraph; show the EXPECTED OUTPUT ("you should see: X").
Fill in every section and remove these guide lines. Mark any uncertainty with [CLARIFY: specific question].
Size it by the task: a simple runbook has short sections — do not inflate. The status (active/superseded) is data
managed by cerne (cerne runbook substituir), not prose. -->

<!-- METADATA (fill in as lines **Date:** / **Owner:**):
Date: YYYY-MM-DD — last review/validation.
Owner: the team or person responsible (who to escalate to). -->

## When to use
<!-- The situation that triggers this runbook: trigger, symptoms or goal. The reader arrives in the middle of an incident
and needs to confirm in seconds that this is the right runbook. -->

## Prerequisites
<!-- What to have before starting: access/tokens, tools and versions, environment state, window/approvals.
For the operator to confirm they are ready. -->

## Procedure
<!-- NUMBERED steps, action first, copyable commands. After each step, the expected output ("you should see: X")
so the operator recognizes success. Note idempotency when a step can be safely repeated. -->

## Verification
<!-- How to confirm it worked: observable checks/signals, with the expected output. The final state, unambiguous. -->

## Rollback and troubleshooting
<!-- How to undo/recover if something fails (flag destructive steps). Common failures as a table
symptom → cause → solution. And who to escalate to when the runbook does not resolve it. -->
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

// Stub returns the runbook body cerne scaffolds for a new runbook: the title
// heading followed by the template Body. Callers prepend the cerne_id
// frontmatter.
func Stub(title string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + Body
}
