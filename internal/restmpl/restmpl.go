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

import (
	"regexp"
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/models"
)

// exampleModel is the model id the stamp example shows — the catalog's default,
// so the example never names a model the catalog has retired (F-0311).
var exampleModel = models.Baseline().DefaultModel

// Sections are the canonical result sections, in template order.
var Sections = doclang.Sections(doclang.Result, doclang.PT)

// Body is the result template skeleton: the canonical section headings in order,
// each with inline authoring guidance as HTML comments. It carries no
// frontmatter and no title heading — callers prepend those. doccheck treats
// markdown headings and HTML-comment guidance as scaffold, so a freshly
// scaffolded result reads as a stub (the feature complete gate stays closed)
// until a section is filled. The [ESCLARECER: token is the same ambiguity marker
// the rest of the family uses (spectmpl.AmbiguityMarker), asserted by the tests.
var Body = `<!-- COMO USAR: este é o registro do que a feature ENTREGOU (não o que planejou). Mostre EVIDÊNCIA —
número de testes, arquivo:linha, saída de comando — não afirmação ("pronto" sem prova não vale). A Aceitação
é binária e 1:1 com os critérios da spec. Preencha cada seção e remova estas linhas-guia. Marque qualquer
incerteza com [ESCLARECER: pergunta específica]. Dimensione pelo trabalho: entrega pequena tem seções curtas
— não infle. É o portão de cerne feature complete. -->

<!-- METADADOS (preencha como linhas **Spec:** / **Data:** / **Agente:**):
Spec: link para a spec que esta entrega fecha (../<id>/spec.md) — a fonte dos critérios.
Data: AAAA-MM-DD — quando entregou.
Agente: quem implementou, autodeclarado — "<id do modelo> · esforço <nível na escala do seu harness,
ex.: low/medium/high>"; ex.: "` + exampleModel + ` · esforço high". Sem agente: "humano". Não sabe um dos
dois? marque só ele ("` + exampleModel + ` · esforço desconhecido") — nunca palpite. É registro histórico:
o id concreto do modelo que rodou, não uma categoria genérica. -->

## Resultado
<!-- Em 1-2 frases: o que foi entregue e o estado (entregue / parcial). O resultado de cima, antes do detalhe. -->

## Aceitação
<!-- Cada critério de aceitação da spec → ATENDIDO/PENDENTE, com EVIDÊNCIA (nome do teste, arquivo:linha,
números). Binário e 1:1 com a spec — nada de "parece pronto". Cite "critério N" (forma literal) para cada
critério numerado: é o que o cerne feature complete confere. -->

## O que mudou
<!-- As mudanças concretas que entraram, do ponto de vista de quem lê: arquivos/módulos criados e editados,
comandos novos, comportamento observável. Ancore no real. -->

## Débitos e follow-ups
<!-- O que ficou de fora ou incompleto, dívidas conhecidas e itens adiados — como trabalho concreto (vira
feature/caso novo), não TODO órfão. "Nenhum" quando não houver. -->

## Aprendizados e desvios
<!-- O que aprendeu, o que faria diferente, e desvios intencionais do plano/spec (o que mudou e por quê).
Conciso; "N/A" se nada notável. -->

## Gauntlet
<!-- Registro do loop de comparação; o cerne feature complete recusa o result sem ele. Escreva fora deste
comentário, uma linha por item (as formas literais abaixo, sem o rótulo antes dos dois-pontos):
Linha da barra: Barra: <referência nomeada + acesso (URL, comando ou arquivo)>
Linha de cada rodada: Rodada N — <vitória|derrota|empate>: "<citação curta do veredito do juiz>"
Linha dos achados: Achados do crítico: <n> — corrigidos <…>; refutados <… com file:line>; aceitos como limite <…>
Sem barra aplicável: Barra: não aplicável (<justificativa>)
Dispensa pelo humano: Barra: dispensada por decisão humana (<citação>)
Uma passada só do crítico, sem juiz, vai em "Achados do crítico", nunca numa linha "Rodada N": a linha
"Rodada N" leva o veredito citado do juiz. -->
`

// BodyEN is Body in English (F-0359). The literal forms it shows are the ones
// the result reader accepts (internal/lint/resultgauntlet.go).
var BodyEN = `<!-- HOW TO USE: this is the record of what the feature DELIVERED (not what it planned). Show EVIDENCE —
number of tests, file:line, command output — not assertion ("done" without proof does not count). Acceptance
is binary and 1:1 with the spec criteria. Fill in every section and remove these guide lines. Mark any
uncertainty with [CLARIFY: specific question]. Size it by the work: a small delivery has short sections
— do not inflate. It is the gate of cerne feature complete. -->

<!-- METADATA (fill in as lines **Spec:** / **Date:** / **Agent:**):
Spec: link to the spec this delivery closes (../<id>/spec.md) — the source of the criteria.
Date: YYYY-MM-DD — when it was delivered.
Agent: who implemented it, self-declared — "<model id> · effort <level on your harness's scale,
e.g. low/medium/high>"; e.g. "` + exampleModel + ` · effort high". No agent: "human". Do not know one of the
two? mark only that one ("` + exampleModel + ` · effort unknown") — never guess. It is a historical record:
the concrete id of the model that ran, not a generic category. -->

## Result
<!-- In 1-2 sentences: what was delivered and its state (delivered / partial). The bottom line, before the detail. -->

## Acceptance
<!-- Each acceptance criterion of the spec → MET/UNMET, with EVIDENCE (test name, file:line,
numbers). Binary and 1:1 with the spec — no "looks done". Cite "criterion N" (literal form) for every
numbered criterion: it is what cerne feature complete checks. -->

## What changed
<!-- The concrete changes that went in, from the reader's point of view: files/modules created and edited,
new commands, observable behaviour. Anchor it in what is real. -->

## Debts and follow-ups
<!-- What was left out or incomplete, known debts and deferred items — as concrete work (it becomes a new
feature/case), not an orphan TODO. "None" when there is nothing. -->

## Learnings and deviations
<!-- What you learned, what you would do differently, and intentional deviations from the plan/spec (what changed and why).
Concise; "N/A" if nothing notable. -->

## Gauntlet
<!-- Record of the comparison loop; cerne feature complete refuses the result without it. Write outside this
comment, one line per item (the literal forms below, without the label before the colon):
Bar line: Bar: <named reference + access (URL, command or file)>
Line of each round: Round N — <win|loss|tie>: "<short quote of the judge's verdict>"
Findings line: Critic findings: <n> — fixed <…>; refuted <… with file:line>; accepted as a limit <…>
No applicable bar: Bar: not applicable (<justification>)
Waived by the human: Bar: waived by human decision (<quote>)
A critic-only pass, with no judge, goes in "Critic findings", never on a "Round N" line: the
"Round N" line carries the judge's quoted verdict. -->
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

// Stub returns the result body cerne scaffolds when a feature enters
// development: the title heading followed by the template Body. Callers prepend
// the cerne_id frontmatter.
func Stub(title string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + Body
}

var (
	commentRe   = regexp.MustCompile(`(?s)<!--.*?-->`)
	agentLineRe = regexp.MustCompile(`(?i)\b(?:agente|agent)[*_]*:`)
	effortRe    = regexp.MustCompile(`(?i)\b(?:esfor(?:ç|c\x{0327}|c)o|effort)[ \t]+([\pL\pM\pN-]+)`)
)

// DeclaredEffort reads the effort the agent declared about itself on the
// result's Agente/Agent metadata line (F-0279) — the first "esforço <nível>"
// or "effort <level>" after the label, lower-cased. "" when no such line sits
// above the first "## " section, when it names no effort, or when it says
// desconhecido/unknown: nothing is inferred (F-0391).
func DeclaredEffort(result []byte) string {
	for _, line := range strings.Split(commentRe.ReplaceAllString(string(result), ""), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "## ") {
			break
		}
		loc := agentLineRe.FindStringIndex(line)
		if loc == nil {
			continue
		}
		m := effortRe.FindStringSubmatch(line[loc[1]:])
		if m == nil {
			return ""
		}
		if e := strings.ToLower(m[1]); e != "desconhecido" && e != "unknown" {
			return e
		}
		return ""
	}
	return ""
}
