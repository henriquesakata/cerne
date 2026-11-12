package doccheck

import (
	"strconv"
	"strings"
	"unicode"
)

// ptWords are Portuguese function words and verbs that an English message
// never carries as a whole token — the diacritic check alone misses
// unaccented pt-BR like "aceita(s)" or "informe um <id>". English homographs
// ("no", "do", "com" of ".com") are left out on purpose.
var ptWords = map[string]bool{
	"um": true, "uma": true, "ou": true, "para": true, "sem": true, "nenhum": true, "nenhuma": true,
	"de": true, "da": true, "das": true, "dos": true, "na": true, "em": true, "que": true,
	"pelo": true, "pela": true, "seu": true, "sua": true, "deve": true, "pode": true, "ainda": true,
	"foi": true, "ser": true, "informe": true, "erro": true, "falha": true, "arquivo": true,
	"aceita": true, "aceitando": true,
}

// PTReason says why s reads as pt-BR, or "" when it does not. It is the one
// detector behind both the runtime-output guard (F-0124) and the spec lint
// spec-cli-literal-pt (F-0353): what the guard accepts in code is what the
// lint accepts in a spec.
func PTReason(s string) string {
	for _, r := range s {
		if unicode.Is(unicode.Latin, r) && r > 127 {
			return "diacritic " + strconv.QuoteRune(r)
		}
	}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if ptWords[w] {
			return "pt-BR word " + strconv.Quote(w)
		}
	}
	return ""
}
