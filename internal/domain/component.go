package domain

import (
	"slices"
	"strings"
)

// NearComponent reports the component of vocab that name is too close to, so
// a writer can refuse it and suggest the existing one (E-0005 criterion 21):
// the same root (SameComponentRoot), or an edit distance tiered by the shorter
// name — up to two characters when both have 6 letters or more, one with 4 or
// 5, none with 3 or fewer (ci/ui, git/go are different tools). A name already
// in vocab is not near: it is the existing one. Ties go to the same root, then
// the smallest distance, then the alphabetical first.
func NearComponent(name string, vocab []string) (string, bool) {
	if slices.Contains(vocab, name) {
		return "", false
	}
	n := componentKey(name)
	best, bestDist := "", -1
	for _, v := range vocab {
		d := 0
		if !SameComponentRoot(name, v) {
			k := componentKey(v)
			d = editDistance(n, k)
			if d > nearLimit(min(len(n), len(k))) {
				continue
			}
		}
		if bestDist < 0 || d < bestDist || (d == bestDist && v < best) {
			best, bestDist = v, d
		}
	}
	return best, bestDist >= 0
}

// SameComponentRoot reports whether a and b are equal once lowercased, with
// '-' and '_' dropped, and with an optional final "ing", "es" or "s" removed
// from either side: test/tests/testing, tool/tools/tooling, class/classes.
func SameComponentRoot(a, b string) bool {
	sb := componentStems(componentKey(b))
	for _, s := range componentStems(componentKey(a)) {
		if slices.Contains(sb, s) {
			return true
		}
	}
	return false
}

func componentKey(name string) string {
	return strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(name))
}

// componentStems is the key and each non-empty key without a final suffix.
func componentStems(key string) []string {
	out := []string{key}
	for _, suf := range []string{"ing", "es", "s"} {
		if s, ok := strings.CutSuffix(key, suf); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// nearLimit is the largest edit distance that still counts as near for a
// shorter name of n letters.
func nearLimit(n int) int {
	switch {
	case n >= 6:
		return 2
	case n >= 4:
		return 1
	}
	return -1
}

// editDistance is the Levenshtein distance between two ASCII slugs.
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
