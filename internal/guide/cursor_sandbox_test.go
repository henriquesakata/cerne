package guide

import (
	"strings"
	"testing"
)

// F-0512 crit. 6 (N-0060): asking how cerne works inside the Cursor sandbox
// answers with the Cursor sandbox FAQ entry, which names the files and the
// --network command.
func TestGuideCursorSandboxFirstSection(t *testing.T) {
	for _, q := range []string{"how do I make cerne work inside the cursor sandbox?", "cursor network", "cursor mcp"} {
		got := Search(Corpus(testTree()), q, 3)
		if len(got) == 0 {
			t.Fatalf("no section for %q", q)
		}
		first := got[0]
		if first.Source != "faq" || !strings.Contains(first.Title, "Cursor") || !strings.Contains(first.Title, "sandbox") {
			t.Errorf("%q: first section = %q (%s), want the Cursor sandbox FAQ entry", q, first.Title, first.Source)
			continue
		}
		for _, want := range []string{".cursor/sandbox.json", ".cursor/mcp.json", "cerne hook install --provider cursor --network", "networkPolicy.allow"} {
			if !strings.Contains(first.Text, want) {
				t.Errorf("%q: entry lacks %q", q, want)
			}
		}
	}
}
