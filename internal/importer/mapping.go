package importer

import (
	"regexp"
	"strings"
)

// State heuristics scan a bounded prefix of each document so a stray keyword
// deep in the body never flips the mapped state.
const (
	featureStateScanLines  = 40
	decisionStateScanLines = 10
)

var (
	// reCaseDir matches a case folder name: NNNN_kebab-name.
	reCaseDir = regexp.MustCompile(`^(\d+)_(.+)$`)
	// reADRFile matches an ADR filename: adr-NNN-kebab-title.md.
	reADRFile = regexp.MustCompile(`^adr-(\d+)-(.+)\.md$`)
	// reFeatureSuperseded marks a spec whose status line says the case was
	// superseded, discontinued, absorbed or cancelled. Anchored to the real
	// `**Status:**` metadata line (line start, optional bold, colon) so a
	// stray keyword in a table cell or prose never flips the state.
	reFeatureSuperseded = regexp.MustCompile(`(?im)^\s*\**\s*status\**\s*:.*(supersed|descontinuad|absorvid|cancelad)`)
	// reFeatureImplemented marks a spec whose status line says the case shipped.
	reFeatureImplemented = regexp.MustCompile(`(?im)^\s*\**\s*status\**\s*:.*(done|conclu[íi]d|completo|merged|implementad)`)
	// reDecisionSuperseded marks an ADR superseded by a newer one: either a
	// status line carrying the keyword, or a directed "Superseded by /
	// Substituído por" callout (e.g. the Nygard `> Superseded by adr-NNN`
	// convention). A bare prose mention of the keyword is not enough.
	reDecisionSuperseded = regexp.MustCompile(`(?im)^\s*>?\s*\**\s*(status\**\s*:.*?(supersed|substitu[íi]d)|(supersed|substitu[íi]d)\w*\s+(by|por)\b)`)
)

// featureState maps a case to a feature state from its spec body (first 40
// lines, frontmatter included) and the presence of plan.md / result.md.
func featureState(specBody []byte, hasPlan, hasResult bool) string {
	head := headLines(specBody, featureStateScanLines)
	switch {
	case reFeatureSuperseded.MatchString(head):
		return "superseded"
	case reFeatureImplemented.MatchString(head) || hasResult:
		return "implemented"
	case hasPlan:
		return "in_development"
	default:
		return "candidate"
	}
}

// decisionState maps an ADR body (first 10 lines) to active or superseded.
func decisionState(body []byte) string {
	if reDecisionSuperseded.MatchString(headLines(body, decisionStateScanLines)) {
		return "superseded"
	}
	return "active"
}

// headLines returns the first n lines of body joined by "\n".
func headLines(body []byte, n int) string {
	lines := strings.Split(string(body), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// firstHeading returns the text of the first "# " level-1 heading, skipping
// a leading frontmatter block; "" when the document has none.
func firstHeading(body []byte) string {
	lines := strings.Split(strings.TrimPrefix(string(body), bom), "\n")
	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for j := 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "---" {
				start = j + 1
				break
			}
		}
	}
	for _, line := range lines[start:] {
		if title, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok {
			return strings.TrimSpace(title)
		}
	}
	return ""
}

// humanize converts a kebab/snake name into a display title:
// "export-csv" -> "Export csv".
func humanize(name string) string {
	s := strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(name))
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}
