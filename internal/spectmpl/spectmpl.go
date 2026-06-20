// Package spectmpl holds the canonical spec template that cerne scaffolds into
// a new feature and asks the LLM to fill. It is the single source of truth for
// the spec structure, shared by the stub writer (internal/app) and the
// spec-draft prompt (internal/cli) so the two never drift.
//
// The section order is load-bearing, not cosmetic: the Problema and the
// acceptance criteria sit at the top and Verificação near the end, so the most
// testable, decision-bearing content lands where long-context models attend
// best (primacy/recency) rather than buried in the middle. The rationale and
// its sources are registered in the cerne backend as references (R-0001…).
package spectmpl

import "strings"

// Sections are the canonical spec sections, in template order.
var Sections = []string{
	"Problema",
	"Objetivo e critérios de aceitação",
	"Decisões do design",
	"Desenho",
	"Verificação",
	"Fora de escopo",
}

// AmbiguityMarker is the token an author (or the LLM) leaves in place of a
// guess when something is unresolved, so it surfaces instead of becoming a
// silent wrong assumption.
const AmbiguityMarker = "[ESCLARECER:"

// Body is the spec template skeleton: the canonical section headings in order,
// each with inline authoring guidance as HTML comments. It carries no
// frontmatter and no title heading — callers prepend those. doccheck treats
// markdown headings and HTML-comment guidance as scaffold, so a freshly
// scaffolded spec still reads as a stub (gates stay closed) until a section is
// filled with real content.
const Body = `<!-- COMO USAR: preencha cada seção e remova estas linhas-guia. Marque qualquer
incerteza com [ESCLARECER: pergunta específica] e não prossiga adivinhando. Antes de
cerne feature approve: nenhum [ESCLARECER] restante; critérios testáveis; nada ambíguo. -->

<!-- METADADOS (preencha como linhas **Status:** / **Tipo:** / **Origem:**):
Status: Rascunho (AAAA-MM-DD) — spec para revisão.
Tipo: Novo recurso | Corretivo | Refactor — o caráter da mudança em uma linha.
Origem: o que disparou (relato de agente, dor recorrente, decisão do usuário + data). -->

## Problema
<!-- O que falta ou quebra HOJE, ancorado em código real (arquivo.go:linha). Por que agora;
o custo de não resolver. Só o problema — a solução vem depois. -->

## Objetivo e critérios de aceitação
<!-- Uma frase de objetivo; depois critérios NUMERADOS e testáveis (1 critério : 1 teste).
EARS para comportamento: QUANDO <condição> O SISTEMA DEVE <comportamento observável>.
Invariantes estruturais entram como asserção direta e mensurável. -->

## Decisões do design
<!-- Tabela | Fork (a questão) | Decisão | Razão |; registre também as alternativas rejeitadas. -->

## Desenho
<!-- Por módulo/pacote, ancorado em arquivos reais, com contratos explícitos (assinaturas,
schemas, endpoints, eventos). Inclua o HOW — não o omita. -->

## Verificação
<!-- Plano de testes (TDD), 1:1 com os critérios de aceitação. -->

## Fora de escopo
<!-- O que NÃO será feito agora e por quê (ou o que vira follow-up). -->
`

// Stub returns the spec body cerne scaffolds for a new feature: the title
// heading followed by the template Body. Callers prepend the cerne_id
// frontmatter.
func Stub(title string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + Body
}
