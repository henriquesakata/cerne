package cli

import (
	"strings"

	"github.com/henriquesakata/cerne/internal/doclang"
)

// The draft prompts are written once, in English (N-0068); the document comes
// out in the project's documentation language l (F-0447). What a prompt may not
// do is spell a protocol form by hand: section headings come from
// doclang.Sections and everything else from doclang.Directive, which closes the
// prompt in l.

// writeSections lists the headings of kind k in l, in template order.
func writeSections(b *strings.Builder, k doclang.Kind, l doclang.Lang) {
	for _, s := range doclang.Sections(k, l) {
		b.WriteString("## " + s + "\n")
	}
}

// heading is section i of kind k in language l, quoted for an authoring rule.
func heading(k doclang.Kind, l doclang.Lang, i int) string {
	return `"` + doclang.Sections(k, l)[i] + `"`
}
