package e2e_test

import (
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/doclang"
)

// completeSpec appends every template section the spec lacks (F-0435: the
// approve refuses a spec without them), so fixtures keep testing their gate.
func completeSpec(body string) string {
	if !doccheck.HasRealContent([]byte(body)) {
		return body // a stub stays a stub: the document gate refuses it first
	}
	for _, h := range doccheck.MissingSections([]byte(body), doclang.Spec, doclang.PT) {
		body += "\n## " + h + "\n\nN/A.\n"
	}
	return body
}
