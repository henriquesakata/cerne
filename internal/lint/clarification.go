package lint

import (
	"strings"

	"gitlab.com/rockt/cerne/internal/clarify"
)

// clarificationFindings flags every open [ESCLARECER] marker in an authored
// doc (F-0175), plus every marker whose pipes are unescaped inside a table row
// (F-0282). It runs inside the scanned-docs loop, so terminal entities are
// excluded by construction — scannedDocs only yields live ones. Resolved
// [ESCLARECIDO] markers are the answered trail and stay silent for the first
// rule; the second one covers them too, because a sliced cell breaks the
// resolved chip just the same.
func clarificationFindings(d scannedDoc, text string) []Finding {
	var findings []Finding
	for _, c := range clarify.Parse(text) {
		if !c.Resolved {
			detail := "open clarification: " + c.Question
			if c.Question == "" {
				detail = "open clarification (no question)"
			}
			findings = append(findings, Finding{
				Rule:   RuleOpenClarification,
				Entity: d.entity,
				File:   d.rel,
				Detail: detail,
			})
		}
		if inTableRow(text, c.Offset) && hasCellSeparator(c.Raw) {
			findings = append(findings, Finding{
				Rule:   RuleClarifyUnescapedPipe,
				Entity: d.entity,
				File:   d.rel,
				Detail: `marker sliced by a table cell separator, so no answer chip renders — escape its pipes as \|: ` + c.Raw,
			})
		}
	}
	return findings
}

// hasCellSeparator reports whether s carries a pipe GFM would read as a cell
// separator: one no backslash escape consumes. Scanning matters — a doubled
// backslash escapes itself, so the pipe right after it still separates, and a
// plain "strip the escaped pipes" pass would call that one escaped and miss it.
func hasCellSeparator(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++ // the escape consumes the next byte, whatever it is
			continue
		}
		if s[i] == '|' {
			return true
		}
	}
	return false
}

// inTableRow reports whether offset falls on a line that reads as a GFM table
// row.
//
// ponytail: the heuristic is the leading pipe, which GFM makes optional but
// every cerne doc template uses. It neither requires the delimiter row above
// nor looks through a blockquote, so a borderless table goes unflagged and a
// prose line that happens to start with a pipe is flagged for nothing. Both
// ends are cheap here — the finding only ever says "escape this pipe" — so the
// upgrade to a real markdown parse waits for a doc that needs it.
func inTableRow(content string, offset int) bool {
	if offset < 0 || offset > len(content) {
		return false
	}
	start := strings.LastIndexByte(content[:offset], '\n') + 1
	line := content[start:]
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.HasPrefix(strings.TrimSpace(line), "|")
}
