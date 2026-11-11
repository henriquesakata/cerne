package skills

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doccheck"
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
		// F-0336: the card impediments are soft but the agent's to fix.
		"fix every impediment it lists except open clarifications, which you leave for the human with the approve request",
		"`untagged`",
		"`capability-missing`",
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
		"fix the hard impediments it lists",
	} {
		if strings.Contains(c, gone) {
			t.Errorf("skill still carries the old protocol %q", gone)
		}
	}
}

// The skill's fenced Barra: lines are what agents copy; copied verbatim, each
// is still a template and the approve refuses it — the filled example is
// prose of its own, outside the Barra: value (F-0327).
func TestCerneAuthoringBarTemplatesRefusedVerbatim(t *testing.T) {
	n := 0
	for _, line := range strings.Split(All()[0].Content, "\n") {
		if !strings.HasPrefix(line, "Barra:") {
			continue
		}
		n++
		if bar, err := doccheck.SpecBar([]byte("# t\n\n" + line + "\n")); err == nil {
			t.Errorf("template line copied verbatim is accepted as %q", bar)
		}
	}
	if n == 0 {
		t.Fatal("skill has no Barra: template line")
	}
}
