package skills

import (
	"strings"
	"testing"
)

// TestCerneAuthoringDocumentsHandoff pins the F-0339 rule: before ending a
// session that leaves work for another, `cerne work handoff`; the session that
// receives a brief naming a W- starts with `cerne work start … --from <W-id>`.
func TestCerneAuthoringDocumentsHandoff(t *testing.T) {
	c := fullAuthoring(t)
	for _, sub := range []string{"cerne work handoff", "--from <W-id>"} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}

// TestCerneAuthoringDocumentsHandoffCode pins F-0368 crit. 14: the skill says
// to pass only the HO- code and that the receiver takes it first.
func TestCerneAuthoringDocumentsHandoffCode(t *testing.T) {
	c := fullAuthoring(t)
	for _, sub := range []string{
		"Passing the baton? `cerne work handoff W-… --next …` and pass only the HO- code.",
		"Received an HO-? First action: `cerne handoff take HO-…`.",
	} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}

// TestCerneAuthoringDocumentsReadBack pins F-0368 crit. 18: the giver writes
// the contingencies in --if, the receiver records its read-back in --note.
func TestCerneAuthoringDocumentsReadBack(t *testing.T) {
	c := fullAuthoring(t)
	for _, sub := range []string{
		`--if "<condition> → <action>"`,
		`cerne handoff take HO-… --note "<what you understood + first action>"`,
	} {
		if !strings.Contains(c, sub) {
			t.Errorf("skill missing %q", sub)
		}
	}
}
