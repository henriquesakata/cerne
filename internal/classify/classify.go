// Package classify infers the single declared module a module-less case
// belongs to, from high-confidence signals already present in the imported
// data: the case folder name and the content's tags line. It is deliberately
// conservative — it assigns a module only when exactly one matches — so a
// wrong guess never pollutes the wiki under the wrong module.
package classify

import (
	"strings"

	"gitlab.com/rockt/cerne/internal/slug"
)

// InferModule returns the single declared module that the case points to via
// EITHER a whole-word token in the last path segment of importedFrom OR a
// "#<module>" tag in the content's tags line, or ("", false) when zero or more
// than one distinct module matches (genuinely cross-cutting / ambiguous).
//
// Matching is case- and accent-insensitive (slug.Fold). The leading numeric
// case id ("0021") is just one token among others; tokens that match no module
// are ignored.
func InferModule(importedFrom, content string, modules []string) (string, bool) {
	if len(modules) == 0 {
		return "", false
	}
	candidates := map[string]bool{}

	folderTokens := tokenize(lastSegment(importedFrom))
	tagTokens := tagTokens(content)
	for _, m := range modules {
		key := slug.Fold(m)
		if folderTokens[key] || tagTokens[key] {
			candidates[m] = true
		}
	}

	if len(candidates) != 1 {
		return "", false
	}
	for m := range candidates {
		return m, true
	}
	return "", false
}

// lastSegment returns the final non-empty path segment of a slash path,
// tolerating a trailing slash ("docs/.../0021_finance/" -> "0021_finance").
func lastSegment(path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

// tokenize splits s on runs of non-alphanumeric runes and returns the folded
// tokens as a set ("0021_finance-consolidation" -> {0021, finance, consolidation}).
func tokenize(s string) map[string]bool {
	out := map[string]bool{}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return !isAlphaNum(r)
	}) {
		out[slug.Fold(tok)] = true
	}
	return out
}

// tagTokens parses the first line starting with "tags:" and returns the folded
// tag names with their leading "#" stripped, as a set; empty when there is no
// tags line.
func tagTokens(content string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "tags:")
		if !ok {
			continue
		}
		for _, tag := range strings.Fields(rest) {
			tag = strings.TrimPrefix(tag, "#")
			if tag != "" {
				out[slug.Fold(tag)] = true
			}
		}
		break // only the first tags line counts
	}
	return out
}

// isAlphaNum reports whether r is an ASCII letter/digit or a non-ASCII letter
// (so accented module tokens like "Consolidação" stay whole before folding).
func isAlphaNum(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r >= 0x80: // keep accented Latin runes within a token
		return true
	default:
		return false
	}
}
