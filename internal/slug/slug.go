// Package slug converts human-readable names into filesystem-safe slugs.
package slug

import "strings"

// diacritics maps accented Latin runes to their ASCII base so pt-BR titles
// keep their letters in slugs ("Consolidação" -> "consolidacao").
var diacritics = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

// Fold lowercases s and transliterates Latin diacritics to their ASCII base,
// WITHOUT removing spaces or punctuation. It is the canonical key for matching
// human-entered names case- and accent-insensitively (e.g. "BILLS" and
// "Aplicação" fold to "bills"/"aplicacao"); unlike Make it preserves the full
// string so distinct names stay distinct.
func Fold(s string) string {
	return diacritics.Replace(strings.ToLower(s))
}

// Make lowercases name, transliterates Latin diacritics, maps spaces to "-"
// and strips every remaining rune outside [a-z0-9-].
func Make(name string) string {
	s := Fold(name)
	s = strings.ReplaceAll(s, " ", "-")
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
