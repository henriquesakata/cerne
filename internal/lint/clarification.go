package lint

import (
	"gitlab.com/rockt/cerne/internal/clarify"
)

// clarificationFindings flags every open [ESCLARECER] marker in an authored
// doc (F-0175). It runs inside the scanned-docs loop, so terminal entities are
// excluded by construction — scannedDocs only yields live ones. Resolved
// [ESCLARECIDO] markers are the answered trail and stay silent.
func clarificationFindings(d scannedDoc, text string) []Finding {
	var findings []Finding
	for _, c := range clarify.Open(text) {
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
	return findings
}
