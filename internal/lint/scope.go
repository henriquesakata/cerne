package lint

import "strings"

// EntityOf returns the entity a finding is about: Finding.Entity when set,
// else the id of a content/features/<ID>/… or content/errata/<ID>.md path,
// else "".
func EntityOf(f Finding) string {
	if f.Entity != "" {
		return f.Entity
	}
	if rest, ok := strings.CutPrefix(f.File, "content/features/"); ok {
		if id, _, found := strings.Cut(rest, "/"); found {
			return id
		}
	}
	if rest, ok := strings.CutPrefix(f.File, "content/errata/"); ok {
		return strings.TrimSuffix(rest, ".md")
	}
	return ""
}

// Scope keeps the findings whose entity is in ids, preserving order, and
// counts the ones left out.
func Scope(findings []Finding, ids map[string]bool) (in []Finding, outside int) {
	for _, f := range findings {
		if ids[EntityOf(f)] {
			in = append(in, f)
		} else {
			outside++
		}
	}
	return in, outside
}
