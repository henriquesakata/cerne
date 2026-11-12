package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/lint"
)

// F-0352: `next --json` and `lint --json` rows carry Fix (omitempty).
func TestNextFindingSerializesFix(t *testing.T) {
	b, _ := json.Marshal(newNextFinding(lint.Finding{Rule: "broken-link", Detail: "d", Line: 3, Fix: "cerne feature errata F-1 'x'"}))
	if !strings.Contains(string(b), `"fix":"cerne feature errata F-1 'x'"`) || !strings.Contains(string(b), `"line":3`) {
		t.Fatalf("row = %s", b)
	}
	b, _ = json.Marshal(newNextFinding(lint.Finding{Rule: "r", Detail: "d"}))
	if strings.Contains(string(b), "fix") || strings.Contains(string(b), "line") {
		t.Fatalf("empty fix/line must be omitted: %s", b)
	}
}
