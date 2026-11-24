package importer

import (
	"os"
	"path/filepath"
	"testing"
)

// F-0500 crit. 5: a trivial snippet is not proof; it needs 12 runes once
// normalized, even when it appears in the file.
func TestAnchorPlanItemsEvidenceTooShort(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "H.md"), []byte("a\nthe cache  was\nremoved today\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	item := func(title, ev string) PlanItem {
		return PlanItem{Kind: "note", Title: title, State: "active", Body: "b", Source: "H.md", Evidence: []string{ev}}
	}
	anchored, un := reasons(t, root, []PlanItem{
		item("a", "a"),
		item("the", "the"),
		item("eleven", "removed toda"[:11]), // 11 runes
		item("ok", "the cache was removed"), // 21 runes, across lines and spaces
		item("exact", "cache was re"),       // 13: passes (found)
		item("twelve", "was removed "),      // trims to 11: too short
		item("accent", "ação é ótima"+"!"),  // 13 runes, absent: not found, not short
	})
	if len(anchored) != 2 || anchored[0] != "ok" || anchored[1] != "exact" {
		t.Fatalf("anchored = %v, want [ok exact]", anchored)
	}
	for title, want := range map[string]string{
		"a":      `evidence too short: "a"`,
		"the":    `evidence too short: "the"`,
		"eleven": `evidence too short: "removed tod"`,
		"twelve": `evidence too short: "was removed "`,
	} {
		if un[title] != want {
			t.Errorf("%s: reason = %q, want %q", title, un[title], want)
		}
	}
	if got := un["accent"]; got != `evidence not found in H.md: "ação é ótima!"` {
		t.Errorf("accent: reason = %q", got)
	}
}
