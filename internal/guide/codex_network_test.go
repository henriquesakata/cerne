package guide

import (
	"strings"
	"testing"
)

// F-0484 crit. 6: "codex network" answers with the Codex sandbox/network FAQ
// entry as the first section.
func TestGuideCodexNetworkFirstSectionIsTheSandboxEntry(t *testing.T) {
	got := Search(Corpus(testTree()), "codex network", 3)
	if len(got) == 0 {
		t.Fatal("no section for \"codex network\"")
	}
	first := got[0]
	if !strings.HasPrefix(first.Source, "faq") || !strings.Contains(first.Title, "open the network") {
		t.Errorf("first section = %q (%s), want the Codex sandbox FAQ entry about opening the network", first.Title, first.Source)
	}
}
