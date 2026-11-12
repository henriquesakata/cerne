package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/lint"
)

// F-0352: the lint API row carries Fix (omitempty) and Line.
func TestLintFindingSerializesFix(t *testing.T) {
	b, _ := json.Marshal(newLintFinding(lint.Finding{Rule: "broken-link", Detail: "d", Line: 3, Fix: "cerne feature errata F-1 'x'"}))
	if !strings.Contains(string(b), `"fix":"cerne feature errata F-1 'x'"`) || !strings.Contains(string(b), `"line":3`) {
		t.Fatalf("row = %s", b)
	}
	b, _ = json.Marshal(newLintFinding(lint.Finding{Rule: "r", Detail: "d"}))
	if strings.Contains(string(b), "fix") {
		t.Fatalf("empty fix must be omitted: %s", b)
	}
}
