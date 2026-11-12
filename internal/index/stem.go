package index

import (
	"strings"
	"unicode/utf8"

	"github.com/henriquesakata/cerne/internal/slug"
)

// suffixes is the closed pt-BR + en suffix list of F-0367, unaccented (the
// unicode61 tokenizer matches without diacritics) and longest first. stem
// strips one suffix only.
var suffixes = []string{
	"mente",
	"coes", "adas", "ados",
	"cao", "oes", "ada", "ado", "ias", "ing", "ion",
	"ao", "ia", "ar", "er", "ir", "as", "os", "es", "ed",
	"a", "o", "s",
}

const (
	minStemTerm = 5 // terms shorter than this are never reduced
	minStem     = 4 // a stem keeps at least this many letters
)

// stem reduces a term already folded by slug.Fold to its stem: the term minus
// the longest listed suffix that leaves at least minStem letters. Terms under
// minStemTerm letters come back unchanged. ponytail: a closed suffix list, not
// RSLP (~200 rules); grow it when the relevance fixture shows a miss.
func stem(term string) string {
	n := utf8.RuneCountInString(term)
	if n < minStemTerm {
		return term
	}
	for _, s := range suffixes {
		if strings.HasSuffix(term, s) && n-len(s) >= minStem {
			return strings.TrimSuffix(term, s)
		}
	}
	return term
}

// termQuery is the FTS5 expression for one OR term: the term itself, plus its
// stem as a prefix when the stem differs (troca -> "troca" OR "troc"*). Both
// are quoted, so a term never reaches MATCH as an operator or as syntax.
func termQuery(term string) string {
	if s := stem(slug.Fold(term)); s != slug.Fold(term) {
		return quotePhrase(term) + " OR " + quotePhrase(s) + "*"
	}
	return quotePhrase(term)
}
