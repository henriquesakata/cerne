package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/store"
)

// withSpans wraps plan items as anchored items with no spans.
func withSpans(items []PlanItem) []AnchoredItem {
	out := make([]AnchoredItem, len(items))
	for i, it := range items {
		out[i] = AnchoredItem{PlanItem: it}
	}
	return out
}

// F-0500 crit. 8: the applied entity records where each snippet sits.
func TestApplyPlanRecordsAnchor(t *testing.T) {
	repo := t.TempDir()
	if err := store.InitLayout(repo, store.Project{Name: "t"}); err != nil {
		t.Fatal(err)
	}
	it := validItem("note")
	it.Source = "HISTORY.md#go2rtc"
	items := []AnchoredItem{{PlanItem: it, Spans: []Span{{"HISTORY.md", 12, 14}, {"HISTORY.md", 30, 31}}}}
	if _, err := ApplyPlan(repo, items, "tester"); err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(repo, "content", "notes", "N-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	const want = "HISTORY.md:12-14, HISTORY.md:30-31"
	if got := doccheck.FrontmatterValue(doc, "imported_anchor"); got != want {
		t.Fatalf("imported_anchor = %q, want %q\n%s", got, want, doc)
	}
	if got := doccheck.FrontmatterValue(doc, "imported_from"); got != "HISTORY.md#go2rtc" {
		t.Fatalf("imported_from = %q", got)
	}
	evs, err := store.ReadEvents(repo)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		if strings.HasSuffix(e.Type, ".imported") && e.Payload["anchor"] == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("no .imported event with anchor %q in %+v", want, evs)
	}
}

// A path-based item with no spans keeps the old frontmatter (no anchor line).
func TestApplyPlanWithoutSpansOmitsAnchor(t *testing.T) {
	repo := t.TempDir()
	if err := store.InitLayout(repo, store.Project{Name: "t"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPlan(repo, withSpans([]PlanItem{validItem("note")}), "tester"); err != nil {
		t.Fatal(err)
	}
	doc, _ := os.ReadFile(filepath.Join(repo, "content", "notes", "N-0001.md"))
	if strings.Contains(string(doc), "imported_anchor") {
		t.Fatalf("anchor line present without spans:\n%s", doc)
	}
}
