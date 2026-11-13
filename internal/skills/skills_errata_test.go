package skills

import (
	"regexp"
	"strings"
	"testing"
)

// TestSkillErrataTeachesMarker pins F-0329 crit. 5: the Errata section teaches
// the real way to acknowledge a superseded mention in an append-only errata — a
// new entry carrying the marker, which looks back only — not the old promise
// that an entry pointing at the successor clears it.
func TestSkillErrataTeachesMarker(t *testing.T) {
	_, sec, ok := strings.Cut(fullAuthoring(t), "## Errata (correcting")
	if !ok {
		t.Fatal("skill has no Errata section")
	}
	sec, _, _ = strings.Cut(sec, "\n## ")
	for _, sub := range []string{
		// Single quotes: inside double quotes interactive zsh history-expands
		// the marker's "!" before cerne runs.
		"cerne <kind> errata <id> '<!-- lint:ok-superseded X --> <why the mention is history>'",
		"never covers a later entry",
		// Several ids go in one marker: only the first marker on a line counts.
		"<!-- lint:ok-superseded X, Y -->",
	} {
		if !strings.Contains(sec, sub) {
			t.Errorf("Errata section missing %q", sub)
		}
	}
	for _, old := range []string{
		"points at the successor",
		"cannot carry the `lint:ok-superseded` marker",
		"clears the debt",
		"\"<!-- lint:ok-superseded",
	} {
		if strings.Contains(sec, old) {
			t.Errorf("Errata section still says %q", old)
		}
	}
	// The false promise in any wording: no sentence may say that naming or
	// pointing at the successor clears a finding (spec problem 3).
	for _, sentence := range sentenceSplit.Split(sec, -1) {
		if successorRe.MatchString(sentence) && clearRe.MatchString(sentence) {
			t.Errorf("Errata section still ties the successor to clearing a finding: %q", sentence)
		}
	}
}

var (
	sentenceSplit = regexp.MustCompile(`[.!?]\s+|\n`)
	successorRe   = regexp.MustCompile(`(?i)\bsuccessor`)
	clearRe       = regexp.MustCompile(`(?i)\bclear(s|ed|ing)?\b`)
)
