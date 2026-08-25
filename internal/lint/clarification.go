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
		if inTableRow(text, c.Offset) && strings.Contains(unescapedPipes(c.Raw), "|") {
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

// unescapedPipes blanks out the escaped pipes so a plain Contains finds only
// the ones GFM would read as cell separators.
func unescapedPipes(s string) string { return strings.ReplaceAll(s, `\|`, "") }

// inTableRow reports whether offset falls on a line that reads as a GFM table
// row.
//
// ponytail: the heuristic is the leading pipe, which GFM makes optional but
// every cerne doc template uses. A borderless table would go unflagged — a
// missed warning, never a false one; upgrade to a real markdown parse if
// borderless tables ever show up.
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
