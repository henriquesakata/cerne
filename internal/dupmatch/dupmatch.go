// Package dupmatch holds the single duplicate-detection criteria shared by the
// registration guards (internal/app, F-0147/F-0324) and the duplicate-live lint
// rule (internal/lint): title similarity and normalized-URL equality.
package dupmatch

import (
	"net/url"
	"strings"

	"gitlab.com/rockt/cerne/internal/slug"
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

// SimilarTitles reports whether two titles register the same thing: exact
// normalized equality always matches; otherwise token overlap against the
// smaller set must reach dupOverlapThreshold. Single-token titles only match
// exactly (one shared word is not a duplicate).
func SimilarTitles(a, b string) bool {
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

// NormalizeURL canonicalizes a source URL for equality: lowercase scheme and
// host, no fragment, no trailing slash on the path. Query is kept (it can
// select a different document). An unparsable URL falls back to its trimmed,
// lowercased text.
func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.ToLower(strings.TrimRight(raw, "/"))
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.RawFragment = ""
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return u.String()
}
