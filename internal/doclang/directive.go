package doclang

import (
	"fmt"
	"strings"
)

// AmbiguityMarker is the token an author leaves in place of a guess, in
// language l.
func AmbiguityMarker(l Lang) string {
	if l == EN {
		return "[CLARIFY:"
	}
	return "[ESCLARECER:"
}

// directiveForms are the per-language literals a document carries that are not
// a Term of the bar protocol: the names the prompt cannot say in its own
// language. Everything the Term vocabulary has comes from Display.
var directiveForms = map[Lang]struct {
	name, question, ears, labels, met, unknown, effort, human string
}{
	PT: {
		name:     "Brazilian Portuguese (pt-BR)",
		question: "pergunta específica",
		ears:     "QUANDO <condição> O SISTEMA DEVE <comportamento>",
		labels: "**Tipo:** (Novo recurso | Corretivo | Refactor), **Origem:**, **Nível:** (leve | padrão | avançado), " +
			"**Spec:**, **Branch:**, **Saída (Gauntlet):**, **Data:**, **Agente:**, **Dono:**",
		met:     "ATENDIDO",
		unknown: "desconhecido",
		effort:  "esforço",
		human:   "humano",
	},
	EN: {
		name:     "English",
		question: "specific question",
		ears:     "WHEN <condition> THE SYSTEM SHALL <behaviour>",
		labels: "**Type:** (New feature | Fix | Refactor), **Origin:**, **Level:** (light | standard | advanced), " +
			"**Spec:**, **Branch:**, **Exit (Gauntlet):**, **Date:**, **Agent:**, **Owner:**",
		met:     "MET",
		unknown: "unknown",
		effort:  "effort",
		human:   "human",
	},
}

// Directive closes a draft or chat prompt for a project documented in language
// l (F-0447). The prompts are written once, in English, as instructions to the
// model; what the document must carry — its language and every protocol form
// the lint and the gates read — comes from here, in the project's language, so
// no prompt ever spells such a form by hand. It goes last: the model reads it
// after the instructions and it wins any conflict with them.
func Directive(l Lang) string {
	f, ok := directiveForms[l]
	if !ok {
		l, f = PT, directiveForms[PT]
	}
	d := func(t Term) string { return Display(t, l) }
	var b strings.Builder
	b.WriteString("\n\nDOCUMENT LANGUAGE — this overrides any other hint about the language of the document:\n")
	fmt.Fprintf(&b, "this project's documentation is in %s. Write the WHOLE document in %s, under exactly the section headings listed above, ", f.name, f.name)
	b.WriteString("and write every protocol form below exactly as shown, never translated into another language:\n")
	fmt.Fprintf(&b, "- ambiguity marker: %s %s]\n", AmbiguityMarker(l), f.question)
	fmt.Fprintf(&b, "- %s\n", ClarifyRule(l))
	fmt.Fprintf(&b, "- behaviour criteria (EARS): %s;\n", f.ears)
	fmt.Fprintf(&b, "- metadata labels: %s; an unknown value is %q; the agent line reads \"<model id> · %s <level>\" or %q;\n", f.labels, f.unknown, f.effort, f.human)
	fmt.Fprintf(&b, "- Gauntlet: \"%s:\", \"%s:\", \"%s:\"; the exemptions \"%s: %s (<justification>)\" and \"%s: %s (<quote>)\"; ",
		d(BarLabel), d(MethodLabel), d(ExitLabel), d(BarLabel), d(NotApplicable), d(BarLabel), d(Waived))
	fmt.Fprintf(&b, "a round line of the form: %s N — %s|%s|%s: \"<quote of the judge>\"; \"%s:\";\n",
		d(Round), d(Win), d(Loss), d(Tie), d(CriticFindings))
	fmt.Fprintf(&b, "- evidence: cite \"%s N\" for every numbered criterion; mark each %s or %s.\n", d(Criterion), f.met, d(Unmet))
	b.WriteString("Keep an execution map's \"Etapa N: ID (title)\" lines exactly in that format: it is parsed, not prose.\n")
	return b.String()
}

// Reply closes a prompt whose output is an answer, a title or a page rather
// than a spec-family document: it names the project's language and carries no
// protocol form.
func Reply(l Lang) string {
	f, ok := directiveForms[l]
	if !ok {
		f = directiveForms[PT]
	}
	return "\n\nLANGUAGE — write your whole reply in " + f.name + " (identifiers, commands, file paths and quoted code stay as they are).\n"
}

// Prose is the free text a prompt has the model copy into its output, per
// language (F-0447): the example sentences of the Gauntlet lines, the mark of a
// parallel task, and the words of a roast report. A prompt written in English
// never spells such a sentence itself, or a pt-BR project would get English
// prose inside a form it was told to copy exactly.
type Prose struct {
	Method, Exit, BarExample, Alternative string // the Gauntlet header values and the filled bar example
	Parallel                              string // the mark of a task that does not depend on the previous ones
	Severities, NoFindings                string // the roast severity set and the empty-report sentence
	Covered, Uncovered                    string // the two coverage-matrix verdicts of the plan roast
	WhereIs, HowToAdjust, SourcesRead     string // the three headings of a codex live-query answer
	LevelLine, OriginExample              string // the Level metadata line and the Origin example of a spec
	DecisionVoice, DecisionTitle          string // how an ADR states its decision and titles itself
	ExpectedOutput, Consulted             string // a runbook step's expected output, and the chat's consulted-sources label
	Planning                              string // the word that ends a planning work title, "F-NNNN: <what> — <word>" (F-0475)
}

var proseForms = map[Lang]Prose{
	PT: {
		Method:         "comparação cega, lado a lado, por crítico de contexto fresco que não participou da construção",
		Exit:           "vencer ou empatar; derrota abre nova rodada; parar perdendo só por encerramento humano registrado",
		BarExample:     "modo spec do Kiro — kiro.dev/docs/specs; mesma classe: conduzir humano até spec completa",
		Alternative:    "<teste/benchmark/invariante>",
		Parallel:       "[paralela]",
		Severities:     "[alta|média|baixa]",
		NoFindings:     "sem achados relevantes",
		Covered:        "coberto pela tarefa X",
		Uncovered:      "descoberto",
		WhereIs:        "Onde está",
		HowToAdjust:    "Para ajustar",
		SourcesRead:    "Fontes consultadas",
		LevelLine:      "**Nível:** <leve|padrão|avançado> — <justificativa em 1 frase>",
		OriginExample:  `**Origem:** "Relato de agente: cerne push falha em worktree paralelo (sessão 2026-05-14)"`,
		DecisionVoice:  `"Vamos X" / "Usaremos X"`,
		DecisionTitle:  `"Usar X", não "Devemos usar X?"`,
		ExpectedOutput: `"você deve ver: X"`,
		Consulted:      "Consultei",
		Planning:       "planejamento",
	},
	EN: {
		Method:         "blind side-by-side comparison by a fresh-context critic who did not take part in the build",
		Exit:           "win or tie; a loss opens a new round; stopping while losing only by a recorded human closure",
		BarExample:     "Kiro's spec mode — kiro.dev/docs/specs; same class: guide a human to a complete spec",
		Alternative:    "<test/benchmark/invariant>",
		Parallel:       "[parallel]",
		Severities:     "[high|medium|low]",
		NoFindings:     "no relevant findings",
		Covered:        "covered by task X",
		Uncovered:      "uncovered",
		WhereIs:        "Where it is",
		HowToAdjust:    "How to adjust",
		SourcesRead:    "Sources consulted",
		LevelLine:      "**Level:** <light|standard|advanced> — <one-sentence justification>",
		OriginExample:  `**Origin:** "Agent report: cerne push fails in a parallel worktree (session 2026-05-14)"`,
		DecisionVoice:  `"We will X" / "We use X"`,
		DecisionTitle:  `"Use X", not "Should we use X?"`,
		ExpectedOutput: `"you should see: X"`,
		Consulted:      "Consulted",
		Planning:       "planning",
	},
}

// Examples returns the free text of language l; a language outside the catalog
// reads as PT, like Sections.
func Examples(l Lang) Prose {
	if p, ok := proseForms[l]; ok {
		return p
	}
	return proseForms[PT]
}

// ClarifyForm is the clarification marker with proposed options and a
// recommended one, written in language l (F-0517). The suffix is the
// project's, like every protocol form (F-0418).
func ClarifyForm(l Lang) string {
	if l == EN {
		return "[CLARIFY: <specific question> | <option A> (recommended) | <option B>]"
	}
	return "[ESCLARECER: <pergunta específica> | <opção A> (recomendada) | <opção B>]"
}

// ClarifyRule is the one sentence that teaches how to ask a clarification
// (F-0517): the possible answers go after the question, one per pipe, the
// author's pick carries the suffix, and a question with no possible answers
// stays without a pipe. The session rule, the drafting prompts and the skill's
// reference carry this same text, with the form in the project's language.
func ClarifyRule(l Lang) string {
	suffix := "(recomendada)"
	if l == EN {
		suffix = "(recommended)"
	}
	return "clarification options: when a question has possible answers, write each one after the question, one per pipe, and end the one you would pick with " + suffix +
		" (at most one; the reason goes in the question): " + ClarifyForm(l) + "; a question with no possible answers stays without a pipe. The person answers with one click and can always write another answer."
}
