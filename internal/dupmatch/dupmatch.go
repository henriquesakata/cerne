// Package dupmatch holds the single duplicate-detection criteria shared by the
// registration guards (internal/app, F-0147/F-0324) and the duplicate-live lint
// rule (internal/lint): title similarity and normalized-URL equality.
package dupmatch

import (
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/henriquesakata/cerne/internal/slug"
)

// dupOverlapThreshold is the token-overlap ratio (against the smaller title)
// at or above which two titles are considered the same registration. 0.8 is
// deliberately conservative: "corrigir bug do lock" vs "corrigir bug do push"
// (same shape, different bug) stays below it, while a shorter duplicate fully
// contained in a longer title hits 1.0.
const dupOverlapThreshold = 0.8

// titleTokens normalizes a title (lowercase, diacritics stripped — the slug
// package owns that) into its token set.
func titleTokens(title string) map[string]bool {
	tokens := map[string]bool{}
	for _, tok := range strings.Split(slug.Make(title), "-") {
		if tok != "" {
			tokens[tok] = true
		}
	}
	return tokens
}

// idToken matches an id-like token (F-0101, RM-0003, W-12, T-0008).
var idToken = regexp.MustCompile(`(?i)\b[A-Z]+-\d+\b`)

// idSet is the normalized (upper-case) set of id-like tokens of a title,
// rendered as a sorted key so two sets compare by equality.
func idSet(title string) string {
	var ids []string
	for _, m := range idToken.FindAllString(title, -1) {
		ids = append(ids, strings.ToUpper(m))
	}
	sort.Strings(ids)
	ids = slices.Compact(ids) // a set: a repeated id is the same id
	return strings.Join(ids, ",")
}

// SimilarTitles reports whether two titles register the same thing: titles that
// both carry id-like tokens (F-0101, RM-0003) with DIFFERENT id sets never
// match — the id is what distinguishes "Lint de F-0101 quebra" from "... F-0110
// ...". Otherwise exact
// normalized equality always matches; otherwise token overlap against the
// smaller set must reach dupOverlapThreshold. Single-token titles only match
// exactly (one shared word is not a duplicate).
func SimilarTitles(a, b string) bool {
	if ia, ib := idSet(a), idSet(b); ia != "" && ib != "" && ia != ib {
		return false
	}
	if slug.Make(a) == slug.Make(b) {
		return true
	}
	ta, tb := titleTokens(a), titleTokens(b)
	minLen := len(ta)
	if len(tb) < minLen {
		minLen = len(tb)
	}
	if minLen < 2 {
		return false
	}
	shared := 0
	for tok := range ta {
		if tb[tok] {
			shared++
		}
	}
	return float64(shared)/float64(minLen) >= dupOverlapThreshold
}

// NormalizeURL canonicalizes a source URL for equality: http and https are the
// same scheme (folded to https), the host is lower-cased and loses a leading
// "www.", the fragment and a trailing slash on the path are dropped. The path
// keeps its case (paths are case-sensitive) and the query is kept (it can
// select a different document). A URL without a scheme is read as https, so
// only its host part is lower-cased. An unparsable URL falls back to its
// trimmed text without a trailing slash.
func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	parse := raw
	if !strings.Contains(parse, "://") {
		parse = "https://" + parse
	}
	u, err := url.Parse(parse)
	if err != nil || u.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	u.Scheme = "https"
	u.Host = strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	u.Fragment = ""
	u.RawFragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return u.String()
}
