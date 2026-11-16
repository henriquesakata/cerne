package cli

import (
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
)

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
	return prompt + doclang.EnglishDirective
}
