package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F-0500 crit. 3, 4, 5: classification by literal anchor, with the reason.
func TestAnchorPlanItems(t *testing.T) {
	root := t.TempDir()
	hist := "# History\n\nIntro line.\n\nWe dropped go2rtc\nafter the  proxy   change.\n\nOther text here.\n"
	if err := os.WriteFile(filepath.Join(root, "HISTORY.md"), []byte(hist), 0o644); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("x", 100)
	item := func(src string, ev ...string) PlanItem {
		return PlanItem{Kind: "note", Title: "T " + src, State: "active", Body: "b", Source: src, Evidence: ev}
	}
	tests := []struct {
		name   string
		item   PlanItem
		reason string // "" = anchored
		spans  []string
	}{
		{"no evidence", item("HISTORY.md"), "no evidence", nil},
		{"too many snippets", item("HISTORY.md", "Intro", "Intro", "Intro", "Intro"), "more than 3 evidence snippets", nil},
		{"source missing", item("GONE.md", "x"), "source not found: GONE.md", nil},
		{"source escapes root", item("../HISTORY.md", "x"), "source not found: ../HISTORY.md", nil},
		{"absolute source", item("/etc/passwd", "root"), "source not found: /etc/passwd", nil},
		{"fragment source", item("HISTORY.md#go2rtc", "We dropped go2rtc"), "", []string{"HISTORY.md:5-5"}},
		{"one good one bad", item("HISTORY.md", "Intro line.", "never written"), `evidence not found in HISTORY.md: "never written"`, nil},
		{"empty snippet", item("HISTORY.md", "  "), `evidence not found in HISTORY.md: "  "`, nil},
		{"long failing snippet is cut", item("HISTORY.md", long), `evidence not found in HISTORY.md: "` + strings.Repeat("x", 80) + `…"`, nil},
		{"different line breaks", item("HISTORY.md", "dropped go2rtc after the proxy change."), "", []string{"HISTORY.md:5-6"}},
		{"two spans", item("HISTORY.md", "Intro line.", "Other text here."), "", []string{"HISTORY.md:3-3", "HISTORY.md:8-8"}},
	}
	items := make([]PlanItem, len(tests))
	for i, tt := range tests {
		items[i] = tt.item
	}
	anchored, unanchored, err := AnchorPlanItems(root, items)
	if err != nil {
		t.Fatal(err)
	}
	var wantA, wantU []string
	for _, tt := range tests {
		if tt.reason == "" {
			wantA = append(wantA, tt.item.Source)
		} else {
			wantU = append(wantU, tt.item.Source+" | "+tt.reason)
		}
	}
	var gotA, gotU []string
	for _, a := range anchored {
		gotA = append(gotA, a.Source)
	}
	for _, u := range unanchored {
		gotU = append(gotU, u.Item.Source+" | "+u.Reason)
	}
	if strings.Join(gotA, "\n") != strings.Join(wantA, "\n") {
		t.Fatalf("anchored = %v, want %v", gotA, wantA)
	}
	if strings.Join(gotU, "\n") != strings.Join(wantU, "\n") {
		t.Fatalf("unanchored =\n%s\nwant\n%s", strings.Join(gotU, "\n"), strings.Join(wantU, "\n"))
	}
	k := 0
	for _, tt := range tests {
		if tt.reason != "" {
			continue
		}
		var got []string
		for _, s := range anchored[k].Spans {
			got = append(got, s.String())
		}
		if strings.Join(got, ",") != strings.Join(tt.spans, ",") {
			t.Errorf("%s: spans = %v, want %v", tt.name, got, tt.spans)
		}
		k++
	}
}

// F-0500 crit. 9: a read error of one item is that item's problem, never a
// global one: the good item next to it stays anchored.
func TestAnchorPlanItemsUnreadableSourceIsPerItem(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("State: the project is active now.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(root, "locked.md")
	if err := os.WriteFile(locked, []byte("secret state line here"), 0o000); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(locked); err == nil { // running as root: no permission error to test
		f.Close()
		os.Remove(locked)
	}
	item := func(src string, ev string) PlanItem {
		return PlanItem{Kind: "note", Title: "T " + src, State: "active", Body: "b", Source: src, Evidence: []string{ev}}
	}
	items := []PlanItem{
		item("CLAUDE.md/x", "anything at all here"), // ENOTDIR
		item("CLAUDE.md", "the project is active now"),
	}
	wantUnreadable := []string{"CLAUDE.md/x"}
	if _, err := os.Stat(locked); err == nil {
		items = append(items, item("locked.md", "secret state line here"))
		wantUnreadable = append(wantUnreadable, "locked.md")
	}
	anchored, unanchored, err := AnchorPlanItems(root, items)
	if err != nil {
		t.Fatalf("a per-item read error became global: %v", err)
	}
	if len(anchored) != 1 || anchored[0].Source != "CLAUDE.md" {
		t.Fatalf("anchored = %+v, want only CLAUDE.md", anchored)
	}
	if len(unanchored) != len(wantUnreadable) {
		t.Fatalf("unanchored = %+v", unanchored)
	}
	for i, u := range unanchored {
		if u.Reason != "source unreadable: "+wantUnreadable[i] {
			t.Errorf("reason = %q, want source unreadable: %s", u.Reason, wantUnreadable[i])
		}
	}
}
