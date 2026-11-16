package doccheck

import (
	"errors"
	"strings"

	"github.com/henriquesakata/cerne/internal/clarify"
	"github.com/henriquesakata/cerne/internal/doclang"
)

// Issue severities and codes of CheckSpecBody. The codes are the approve
// readiness' impediment codes, so one rule speaks one vocabulary (F-0438).
const (
	SeverityError   = "error"
	SeverityWarning = "warning"

	IssueIncompleteSpec = "incomplete-spec"
	IssueBarMissing     = "bar-missing"
	IssueBarMalformed   = "bar-malformed"
	IssueOpenClarify    = "open-clarification"
)

// Issue is one finding of CheckSpecBody.
type Issue struct {
	Code     string   `json:"code"`
	Severity string   `json:"severity"`
	Detail   string   `json:"detail"`
	Missing  []string `json:"missing,omitempty"` // the absent sections, on incomplete-spec
}

// CheckSpecBody is the one check of a spec body: the template sections, the
// Gauntlet bar line (skipped in light mode) and the open clarifications (a
// warning, a legitimate question to the human). The approve readiness, the
// lint and the chat all call it, so they cannot disagree (F-0438).
func CheckSpecBody(body []byte, lang doclang.Lang, light bool) []Issue {
	return CheckBody(body, doclang.Spec, lang, light)
}

// CheckBody is CheckSpecBody for a document kind: a bug report (doclang.Bug)
// has its own sections but the same bar and clarification rules.
func CheckBody(body []byte, kind doclang.Kind, lang doclang.Lang, light bool) []Issue {
	var out []Issue
	if missing := MissingSections(body, kind, lang); len(missing) > 0 {
		out = append(out, Issue{IssueIncompleteSpec, SeverityError, "the spec is missing template sections: " + strings.Join(missing, ", "), missing})
	}
	if _, err := SpecBarIn(body, lang); !light && err != nil {
		code := IssueBarMissing
		if errors.Is(err, ErrBarMalformed) {
			code = IssueBarMalformed
		}
		out = append(out, Issue{Code: code, Severity: SeverityError, Detail: err.Error()})
	}
	for _, c := range clarify.Open(string(body)) {
		q := c.Question
		if q == "" {
			q = c.Raw
		}
		out = append(out, Issue{Code: IssueOpenClarify, Severity: SeverityWarning, Detail: q})
	}
	return out
}
