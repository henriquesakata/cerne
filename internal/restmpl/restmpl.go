// Package restmpl holds the canonical RESULT template that cerne scaffolds when a
// feature starts development and asks the LLM to fill. It is the single source of
// truth for the result-record structure, shared by the stub writer (internal/app)
// and the result-draft prompt (internal/cli) so the two never drift — the fourth
// member of the spectmpl/plantmpl/dectmpl family.
//
// The result closes the lifecycle loop: the spec defines numbered acceptance
// criteria, the plan defines "done", and the result verifies done with evidence.
// The section order is load-bearing: Resultado and Aceitação sit at the top, so
// the outcome and its proof land where long-context models attend best (primacy),
// and the softer Aprendizados e desvios sits last (recency is fine). Show
// evidence, never assertion; keep Aceitação binary and 1:1 with the spec. The
// rationale and its sources are registered in the cerne backend as references.
package restmpl

import "strings"

// Sections are the canonical result sections, in template order.
var Sections = []string{
	"Resultado",
	"Aceitação",
	"O que mudou",
	"Débitos e follow-ups",
	"Aprendizados e desvios",
}

// Body is the result template skeleton: the canonical section headings in order,
// each with inline authoring guidance as HTML comments. It carries no
// frontmatter and no title heading — callers prepend those. doccheck treats
// markdown headings and HTML-comment guidance as scaffold, so a freshly
// scaffolded result reads as a stub (the feature complete gate stays closed)
// until a section is filled. The [ESCLARECER: token is the same ambiguity marker
// the rest of the family uses (spectmpl.AmbiguityMarker), asserted by the tests.
const Body = `<!-- COMO USAR: este é o registro do que a feature ENTREGOU (não o que planejou). Mostre EVIDÊNCIA —
número de testes, arquivo:linha, saída de comando — não afirmação ("pronto" sem prova não vale). A Aceitação
é binária e 1:1 com os critérios da spec. Preencha cada seção e remova estas linhas-guia. Marque qualquer
incerteza com [ESCLARECER: pergunta específica]. Dimensione pelo trabalho: entrega pequena tem seções curtas
— não infle. É o portão de cerne feature complete. -->

<!-- METADADOS (preencha como linhas **Spec:** / **Data:** / **Agente:**):
Spec: link para a spec que esta entrega fecha (../<id>/spec.md) — a fonte dos critérios.
Data: AAAA-MM-DD — quando entregou.
Agente: quem implementou, autodeclarado — "<id do modelo> · esforço <nível na escala do seu harness,
ex.: low/medium/high>"; ex.: "claude-opus-5 · esforço high". Sem agente: "humano". Não sabe um dos
dois? marque só ele ("claude-opus-5 · esforço desconhecido") — nunca palpite. É registro histórico:
o id concreto do modelo que rodou, não uma categoria genérica. -->

## Resultado
<!-- Em 1-2 frases: o que foi entregue e o estado (entregue / parcial). O resultado de cima, antes do detalhe. -->

## Aceitação
<!-- Cada critério de aceitação da spec → ATENDIDO/PENDENTE, com EVIDÊNCIA (nome do teste, arquivo:linha,
números). Binário e 1:1 com a spec — nada de "parece pronto". Cite o número do critério. -->

## O que mudou
<!-- As mudanças concretas que entraram, do ponto de vista de quem lê: arquivos/módulos criados e editados,
comandos novos, comportamento observável. Ancore no real. -->

## Débitos e follow-ups
<!-- O que ficou de fora ou incompleto, dívidas conhecidas e itens adiados — como trabalho concreto (vira
feature/caso novo), não TODO órfão. "Nenhum" quando não houver. -->

## Aprendizados e desvios
<!-- O que aprendeu, o que faria diferente, e desvios intencionais do plano/spec (o que mudou e por quê).
Conciso; "N/A" se nada notável. -->
`

// Stub returns the result body cerne scaffolds when a feature enters
// development: the title heading followed by the template Body. Callers prepend
// the cerne_id frontmatter.
func Stub(title string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + Body
}
