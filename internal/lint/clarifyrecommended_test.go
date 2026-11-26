package lint

import (
	"strings"
	"testing"
)

// F-0517 crit. 2
func TestLintClarifyManyRecommended(t *testing.T) {
	d := scannedDoc{entity: "F-0001", rel: "content/features/F-0001/spec.md"}
	count := func(text string) []Finding {
		var out []Finding
		for _, f := range clarificationFindings(d, text) {
			if f.Rule == RuleClarifyManyRecommended {
				out = append(out, f)
			}
		}
		return out
	}

	got := count("x [CLARIFY: which format? | a (recommended) | b (recommended)] y")
	if len(got) != 1 {
		t.Fatalf("findings = %+v, want one", got)
	}
	if got[0].Entity != "F-0001" || got[0].File != d.rel || !strings.Contains(got[0].Detail, "which format?") {
		t.Errorf("finding = %+v, want the document and the question named", got[0])
	}
	if n := len(count("[CLARIFY: q? | a (recommended) | b]")); n != 0 {
		t.Errorf("one recommended option raised %d findings", n)
	}
	if n := len(count("[CLARIFY: q? | a | b]")); n != 0 {
		t.Errorf("no recommended option raised %d findings", n)
	}
}
