package index

import (
	"strings"
	"unicode"
)

// stopwords are common pt-BR + en function words dropped from a natural-language
// question before it reaches the FTS index, so the query keeps only salient
// terms instead of matching the whole sentence literally.
var stopwords = map[string]bool{
	// pt-BR
	"os": true, "as": true, "um": true, "uma": true, "de": true, "do": true,
	"da": true, "dos": true, "das": true, "em": true, "no": true, "na": true,
	"por": true, "que": true, "qual": true, "quais": true, "como": true,
	"quando": true, "onde": true, "porque": true, "para": true, "com": true,
	"sem": true, "foi": true, "está": true, "são": true, "ser": true,
	"sobre": true, "quanto": true, "será": true, "nos": true, "nas": true,
	"ao": true, "aos": true, "pelo": true, "pela": true,
	// en
	"the": true, "an": true, "of": true, "in": true, "on": true, "for": true,
	"how": true, "what": true, "why": true, "when": true, "where": true,
	"is": true, "are": true, "was": true, "were": true, "to": true,
	"and": true, "or": true, "with": true, "without": true, "about": true,
}

// keywordTerms tokenizes on non-letter/digit runes, lowercases, drops terms
// shorter than 3 runes and stopwords, and dedupes preserving order.
func keywordTerms(query string) []string {
	tokens := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var kept []string
	seen := map[string]bool{}
	for _, tok := range tokens {
		t := strings.ToLower(tok)
		if len([]rune(t)) < 3 || stopwords[t] || seen[t] {
			continue
		}
		seen[t] = true
		kept = append(kept, t)
	}
	return kept
}

// Keywords turns a natural-language question into an FTS5 query: the
// surviving terms of keywordTerms, OR-joined, each 5+ letter term followed by
// its stem prefix (termQuery, F-0367). When nothing survives it
// returns the original query unchanged (Search already has a phrase fallback).
func Keywords(query string) string {
	kept := keywordTerms(query)
	if len(kept) == 0 {
		return query
	}
	for i, t := range kept {
		kept[i] = termQuery(t)
	}
	return strings.Join(kept, " OR ")
}
