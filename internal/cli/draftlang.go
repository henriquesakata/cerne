package cli

import (
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// englishDraftDirective closes a draft prompt for a project documented in
// English (F-0359). The prompts themselves stay pt-BR: they are instructions
// to the model, not text the user reads. What must come out in English is the
// document, with the vocabulary the readers accept.
const englishDraftDirective = `

LANGUAGE — this overrides any instruction above about the language of the document:
this project's documentation is in English. Write the WHOLE document in English, under exactly the English
section headings listed above. Wherever the instructions above name a Portuguese form, write its English form:
- ambiguity marker: [CLARIFY: specific question] (never [ESCLARECER: …]);
- behaviour criteria (EARS): WHEN <condition> THE SYSTEM SHALL <behaviour>;
- metadata labels: **Type:** (New feature | Fix | Refactor), **Origin:**, **Level:** (light | standard | advanced),
  **Spec:**, **Branch:**, **Exit (Gauntlet):**, **Date:**, **Agent:**, **Owner:**; an unknown value is "unknown";
- Gauntlet: "Bar:", "Method:", "Exit:"; the exemptions "Bar: not applicable (<justification>)" and
  "Bar: waived by human decision (<quote>)"; "Round N — win|loss|tie: \"<quote>\""; "Critic findings:";
- evidence: cite "criterion N" for every numbered criterion; mark each MET or UNMET.
Keep an execution map's "Etapa N: ID (title)" lines exactly in that format: it is parsed, not prose.
`

// draftPromptIn adapts a draft prompt for kind k to the documentation
// language l: Portuguese is returned untouched; English gets the English
// section headings in place of the Portuguese ones and the directive above.
func draftPromptIn(l doclang.Lang, k doclang.Kind, prompt string) string {
	if l != doclang.EN {
		return prompt
	}
	pt, en := doclang.Sections(k, doclang.PT), doclang.Sections(k, doclang.EN)
	for i := range pt {
		prompt = strings.ReplaceAll(prompt, "## "+pt[i]+"\n", "## "+en[i]+"\n")
	}
	return prompt + englishDraftDirective
}
