package domain

import (
	"regexp"
	"strings"
)

// reCasePrefix matches a leading "Case NNNN —|–|:|- " label some case specs
// carry in their heading, so a candidate's title compares equal to the backend
// feature registered natively without the label.
var reCasePrefix = regexp.MustCompile(`^Case\s+\d+\s*[—–:-]\s*`)

// reSpaces collapses internal whitespace runs to a single space.
var reSpaces = regexp.MustCompile(`\s+`)

// NormalizeTitle canonicalizes a title for identity comparison: it strips a
// leading "Case NNNN — " label, trims, and collapses internal whitespace runs
// to single spaces. Deterministic — no fuzzy matching. Shared by the import
// guard (F-0045) and the feature-creation duplicate guard (F-0121), so both
// apply the same identity rule.
func NormalizeTitle(s string) string {
	s = reCasePrefix.ReplaceAllString(s, "")
	s = reSpaces.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
