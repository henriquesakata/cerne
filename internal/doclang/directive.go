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
