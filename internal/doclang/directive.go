package doclang

import (
	"fmt"
	"strings"
)

// EnglishDirective closes a draft or chat prompt for a project documented in
// English (F-0359). The prompts themselves stay pt-BR: they are instructions
// to the model, not text the user reads. What must come out in English is the
// document, with the vocabulary the readers accept.
const EnglishDirective = `

LANGUAGE — this overrides any instruction above about the language of the document:
this project's documentation is in English. Write the WHOLE document in English, under exactly the English
section headings listed above. Wherever the instructions above name a Portuguese form, write its English form:
- ambiguity marker: [CLARIFY: specific question] (never [ESCLARECER: …]);
- behaviour criteria (EARS): WHEN <condition> THE SYSTEM SHALL <behaviour>;
- metadata labels: **Type:** (New feature | Fix | Refactor), **Origin:**, **Level:** (light | standard | advanced),
  **Spec:**, **Branch:**, **Exit (Gauntlet):**, **Date:**, **Agent:**, **Owner:**; an unknown value is "unknown";
- Gauntlet: "Bar:", "Method:", "Exit:"; the exemptions "Bar: not applicable (<justification>)" and
  "Bar: waived by human decision (<quote>)"; a round line of the form: Round N — win|loss|tie: "<quote of the judge>"; "Critic findings:";
- evidence: cite "criterion N" for every numbered criterion; mark each MET or UNMET.
Keep an execution map's "Etapa N: ID (title)" lines exactly in that format: it is parsed, not prose.
`

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
	name, question, ears, labels, met, unknown string
}{
	PT: {
		name:     "Brazilian Portuguese (pt-BR)",
		question: "pergunta específica",
		ears:     "QUANDO <condição> O SISTEMA DEVE <comportamento>",
		labels: "**Tipo:** (Novo recurso | Corretivo | Refactor), **Origem:**, **Nível:** (leve | padrão | avançado), " +
			"**Spec:**, **Branch:**, **Saída (Gauntlet):**, **Data:**, **Agente:**, **Dono:**",
		met:     "ATENDIDO",
		unknown: "desconhecido",
	},
	EN: {
		name:     "English",
		question: "specific question",
		ears:     "WHEN <condition> THE SYSTEM SHALL <behaviour>",
		labels: "**Type:** (New feature | Fix | Refactor), **Origin:**, **Level:** (light | standard | advanced), " +
			"**Spec:**, **Branch:**, **Exit (Gauntlet):**, **Date:**, **Agent:**, **Owner:**",
		met:     "MET",
		unknown: "unknown",
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
	fmt.Fprintf(&b, "- metadata labels: %s; an unknown value is %q;\n", f.labels, f.unknown)
	fmt.Fprintf(&b, "- Gauntlet: \"%s:\", \"%s:\", \"%s:\"; the exemptions \"%s: %s (<justification>)\" and \"%s: %s (<quote>)\"; ",
		d(BarLabel), d(MethodLabel), d(ExitLabel), d(BarLabel), d(NotApplicable), d(BarLabel), d(Waived))
	fmt.Fprintf(&b, "a round line of the form: %s N — %s|%s|%s: \"<quote of the judge>\"; \"%s:\";\n",
		d(Round), d(Win), d(Loss), d(Tie), d(CriticFindings))
	fmt.Fprintf(&b, "- evidence: cite \"%s N\" for every numbered criterion; mark each %s or %s.\n", d(Criterion), f.met, d(Unmet))
	b.WriteString("Keep an execution map's \"Etapa N: ID (title)\" lines exactly in that format: it is parsed, not prose.\n")
	return b.String()
}
