package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringBarIsAProposal pins F-0308 crit. 10: the agent's Barra:
// line is a proposal written straight into the spec, the human approve is its
// acceptance, and the readiness check precedes the approve request — the old
// "acceptance before it enters the spec" round-trip is gone.
func TestCerneAuthoringBarIsAProposal(t *testing.T) {
	c := All()[0].Content
	for _, want := range []string{
		"is a proposal",
		"the human's approve is its acceptance",
		"cerne feature approve F-NNNN --check",
		"spec-missing-bar",
		"fix the hard impediments it lists; leave open clarifications for the human, with the approve request",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("skill missing %q", want)
		}
	}
	for _, gone := range []string{
		"needs the human's explicit acceptance before it enters the spec",
		"quote the human's confirming words",
		"until it reports approvable",
		"getting a clean `cerne feature approve F-NNNN --check`",
	} {
		if strings.Contains(c, gone) {
			t.Errorf("skill still carries the old protocol %q", gone)
		}
	}
}
