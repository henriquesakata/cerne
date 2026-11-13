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

import (
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/lang"
)

// Sections are the canonical spec sections, in template order.
var Sections = doclang.Sections(doclang.Spec, doclang.PT)

// AmbiguityMarker is the token an author (or the LLM) leaves in place of a
// guess when something is unresolved, so it surfaces instead of becoming a
// silent wrong assumption.
const AmbiguityMarker = "[ESCLARECER:"

// Levels is the generic suggested-capability vocabulary for the **Nível:**
// metadata line (F-0194): the model class the implementation likely needs,
// never a vendor model name (users run different LLMs; names rot). leve =
// mechanical change, existing pattern, one package; padrão = multi-file with
// tests over established patterns; avançado = new subsystem, open design
// decisions, cross-cutting change. Shared by the template Body and the spec
// draft prompt so the two never drift.
var Levels = []string{"leve", "padrão", "avançado"}

// OrigemExample is the literal filled **Origem:** line the METADADOS guidance
// shows as the shape to copy (F-0280): the field had three categories and no
// form, which the blind judges of F-0279 called its weakest point. Exported so
// the spec-draft prompt cites the same example instead of restating it.
const OrigemExample = `"Relato de agente: cerne push falha em worktree paralelo (sessão 2026-05-14)"`

// UnknownMarker is what an author writes in a METADADOS field whose value they
// do not know — never a guess. Same word the result template uses (restmpl), so
// both halves of the template family speak one vocabulary. The **Status:** field
// is gone for the opposite reason: lifecycle state is data on the card
// (cerne status), and a second copy in prose is documentation that lies.
const UnknownMarker = "desconhecido"

// Body is the spec template skeleton: the canonical section headings in order,
// each with inline authoring guidance as HTML comments. It carries no
// frontmatter and no title heading — callers prepend those. doccheck treats
// markdown headings and HTML-comment guidance as scaffold, so a freshly
// scaffolded spec still reads as a stub (gates stay closed) until a section is
// filled with real content.
const Body = `<!-- COMO USAR: preencha cada seção e remova estas linhas-guia. Marque qualquer
incerteza com [ESCLARECER: pergunta específica] e não prossiga adivinhando. Antes de
cerne feature approve: nenhum [ESCLARECER] restante; critérios testáveis; nada ambíguo. -->

<!-- METADADOS (preencha como linhas **Tipo:** / **Origem:** / **Nível:**):
Tipo: Novo recurso | Corretivo | Refactor — o caráter da mudança em uma linha.
Origem: o que disparou (relato de agente, dor recorrente, decisão do usuário), com quem/onde e
a data; ex.: "Relato de agente: cerne push falha em worktree paralelo (sessão 2026-05-14)".
Nível: leve | padrão | avançado — capacidade de modelo sugerida para implementar + 1 frase
de porquê (leve = mecânica, padrão existente, 1 pacote; padrão = multi-arquivo com testes;
avançado = design novo / cross-cutting). Genérico — nunca nome de modelo/vendor.
Campo que você não sabe: escreva "desconhecido" nele — nunca palpite; sabe só parte, escreva a parte
e "desconhecido" no resto (ex.: "Relato de agente: … (sessão desconhecida)"). Tipo e Nível são fechados
— escolha um dos valores. Estado do ciclo de vida não entra aqui: é dado do card (cerne status). -->

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
<!-- Plano de testes (TDD), 1:1 com os critérios de aceitação.` + gauntletGuidePT + ` -->


## Execução recomendada (agente)
<!-- Capacidade de modelo sugerida (vocabulário genérico, coerente com a linha **Nível:**) e esforço.
Tarefas a manter no mesmo agente (contexto compartilhado) e por quê; tarefas separáveis. -->

## Fora de escopo
<!-- O que NÃO será feito agora e por quê (ou o que vira follow-up). -->
`

// BodyEN is Body in English (F-0359): same sections in the same order, same
// guidance, with the English vocabulary of the protocol (CLARIFY, Bar, the
// metadata labels). What the readers accept is doclang's and doccheck's job.
const BodyEN = `<!-- HOW TO USE: fill in every section and remove these guide lines. Mark any
uncertainty with [CLARIFY: specific question] and do not proceed by guessing. Before
cerne feature approve: no [CLARIFY] left; testable criteria; nothing ambiguous. -->

<!-- METADATA (fill in as lines **Type:** / **Origin:** / **Level:**):
Type: New feature | Fix | Refactor — the nature of the change in one line.
Origin: what triggered it (agent report, recurring pain, user decision), with who/where and
the date; e.g. "Agent report: cerne push fails in a parallel worktree (session 2026-05-14)".
Level: light | standard | advanced — suggested model capability to implement it + 1 sentence
of why (light = mechanical, existing pattern, 1 package; standard = multi-file with tests;
advanced = new design / cross-cutting). Generic — never a model or vendor name.
A field you do not know: write "unknown" in it — never guess; if you know only part, write that part
and "unknown" for the rest (e.g. "Agent report: … (session unknown)"). Type and Level are closed
— pick one of the values. Lifecycle state does not go here: it is card data (cerne status). -->

## Problem
<!-- What is missing or broken TODAY, anchored in real code (file.go:line). Why now;
the cost of not fixing it. The problem only — the solution comes later. -->

## Goal and acceptance criteria
<!-- One sentence of goal; then NUMBERED, testable criteria (1 criterion : 1 test).
EARS for behaviour: WHEN <condition> THE SYSTEM SHALL <observable behaviour>.
Structural invariants go in as a direct, measurable assertion. -->

## Design decisions
<!-- Table | Fork (the question) | Decision | Reason |; record the rejected alternatives too. -->

## Design
<!-- By module/package, anchored in real files, with explicit contracts (signatures,
schemas, endpoints, events). Include the HOW — do not leave it out. -->

## Verification
<!-- Test plan (TDD), 1:1 with the acceptance criteria.` + gauntletGuideEN + ` -->


## Recommended execution (agent)
<!-- Suggested model capability (generic vocabulary, consistent with the **Level:** line) and effort.
Tasks to keep in the same agent (shared context) and why; tasks that can be split. -->

## Out of scope
<!-- What will NOT be done now and why (or what becomes a follow-up). -->
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

// StubWithContextIn is StubWithContext in language l. The English body
// already cites the English markers, so no detection is involved.
func StubWithContextIn(l doclang.Lang, title, context string) string {
	if l != doclang.EN {
		return StubWithContext(title, context)
	}
	context = strings.TrimSpace(context)
	if context == "" {
		return StubIn(l, title)
	}
	next := "\n## " + doclang.Sections(doclang.Spec, l)[1] + "\n"
	return strings.Replace(StubIn(l, title), next, "\n"+context+"\n"+next, 1)
}

// Stub returns the spec body cerne scaffolds for a new feature: the title
// heading followed by the template Body. Callers prepend the cerne_id
// frontmatter.
func Stub(title string) string {
	return "# " + strings.TrimSpace(title) + "\n\n" + Body
}

// enMarkers swaps the marker words the guidance cites for the en vocabulary.
// Only the marker words change — the guidance prose stays pt-BR (translating
// the templates is its own feature; the words are what the approve gate and
// the filling agent key on).
var enMarkers = strings.NewReplacer("[ESCLARECER", "[CLARIFY", "[ESCLARECIDO", "[CLARIFIED")

// StubWithContext returns Stub(title) with the Problema section seeded with
// context — the creation-time summary that must survive the session that
// minted the feature (F-0120). The rest of the template stays for the author
// to fill. An empty context degrades to the plain Stub.
//
// The context is also the language signal (F-0182): an English context gets
// the guidance citing [CLARIFY: …], so the agent filling the spec authors
// markers in the doc's own language. Detection is scaffold-only — the parser
// accepts the whole catalog regardless (internal/clarify).
func StubWithContext(title, context string) string {
	context = strings.TrimSpace(context)
	if context == "" {
		return Stub(title)
	}
	next := "\n## " + Sections[1] + "\n"
	s := strings.Replace(Stub(title), next, "\n"+context+"\n"+next, 1)
	if lang.Detect(context) == "en" {
		s = enMarkers.Replace(s)
	}
	return s
}

// The Gauntlet sentence of the Verificação guide, split out so a light-mode
// project (F-0364) gets a scaffold that does not ask for a bar.
const (
	gauntletGuidePT = ` Padrão de parada (Gauntlet),
uma das três formas: Barra (referência nomeada + acesso) + Método (comparação cega, crítico
fresco) + Saída (vencer/empatar; parar perdendo só por encerramento humano); "Barra: não
aplicável (<justificativa de 1 linha>) — padrão alternativo: <teste/benchmark/invariante>";
ou "Barra: dispensada por decisão humana". Com barra nomeada, logo abaixo de Saída: vai
"Pergunta ao juiz: <a pergunta que o juiz cego responde>".
Quando vale o Gauntlet: mudança de comportamento, de contrato, de formato persistido ou de UI
com escolha de desenho. Quando os testes bastam (efeito único e local, ou correção com teste de
regressão): "Barra: não aplicável (<por que os testes bastam>)" — o approve --check mostra o
custo medido do Gauntlet no projeto.`
	gauntletGuideEN = ` Stopping standard (Gauntlet),
one of three forms: Bar (named reference + access) + Method (blind comparison, fresh
critic) + Exit (win/tie; stopping while losing only by a recorded human decision); "Bar: not
applicable (<one-line justification>) — alternative standard: <test/benchmark/invariant>";
or "Bar: waived by human decision (<quote of the words that waived it>)". With a named bar,
right below Exit: goes "Question for the judge: <the question the blind judge answers>".
When the Gauntlet pays: a change of behaviour, of contract, of persisted format, or of UI with a
design choice. When the tests suffice (a single, local effect, or a fix with a regression test):
"Bar: not applicable (<why the tests suffice>)" — approve --check shows the Gauntlet cost
measured in the project.`
)

var gauntletGuides = strings.NewReplacer(gauntletGuidePT, "", gauntletGuideEN, "")

// WithoutGauntlet drops the Gauntlet guidance from a scaffolded spec, for a
// project in light mode ([gates] gauntlet = "off", F-0364). Text the author
// wrote is left alone: only the template's own sentence matches.
func WithoutGauntlet(spec string) string {
	return gauntletGuides.Replace(spec)
}
