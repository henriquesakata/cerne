package importer

import (
	"os"
	"path/filepath"
	"testing"
)

func reasons(t *testing.T, root string, items []PlanItem) (anchored []string, unanchored map[string]string) {
	t.Helper()
	a, u, err := AnchorPlanItems(root, items)
	if err != nil {
		t.Fatalf("AnchorPlanItems: %v", err)
	}
	unanchored = map[string]string{}
	for _, x := range a {
		anchored = append(anchored, x.Title)
	}
	for _, x := range u {
		unanchored[x.Item.Title] = x.Reason
	}
	return anchored, unanchored
}

func trustItem(title, src string) PlanItem {
	return PlanItem{Kind: "note", Title: title, State: "active", Body: "b", Source: src, Evidence: []string{"the state is stated here"}}
}

// F-0500 crit. 4: a source must resolve, symlinks included, to a regular file
// inside root; anything else is "not found" and is never read.
func TestAnchorPlanItemsTrustBoundary(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("the state is stated here"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("intro\nthe state is stated here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{
		"CLAUDE.md": "AGENTS.md",                         // inside → inside: valid
		"leak.md":   filepath.Join(outside, "secret.md"), // out of root
		"dirlink":   outside,                             // dir out of root
	} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	items := []PlanItem{
		trustItem("inside", "CLAUDE.md"),
		trustItem("leak", "leak.md"),
		trustItem("viadir", "dirlink/secret.md"),
		trustItem("dir", "docs"),
	}
	anchored, un := reasons(t, root, items)
	if len(anchored) != 1 || anchored[0] != "inside" {
		t.Fatalf("anchored = %v, want [inside]", anchored)
	}
	for title, src := range map[string]string{"leak": "leak.md", "viadir": "dirlink/secret.md", "dir": "docs"} {
		if un[title] != "source not found: "+src {
			t.Errorf("%s: reason = %q", title, un[title])
		}
	}
}

// F-0500 crit. 4: a file over the cap is left out, never read; the reason is
// cached with the file, so a second item of it is not re-read.
func TestAnchorPlanItemsTooLarge(t *testing.T) {
	root := t.TempDir()
	big := filepath.Join(root, "BIG.md")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxSourceBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	anchored, un := reasons(t, root, []PlanItem{trustItem("one", "BIG.md#a"), trustItem("two", "BIG.md#b")})
	if len(anchored) != 0 {
		t.Fatalf("anchored = %v", anchored)
	}
	for _, title := range []string{"one", "two"} {
		if un[title] != "source too large: BIG.md" {
			t.Errorf("%s: reason = %q", title, un[title])
		}
	}
}

// Several items of one file all anchor, each with its own lines.
func TestAnchorPlanItemsSameFileManyItems(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "H.md"), []byte("first line here\nthe state is stated here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _, err := AnchorPlanItems(root, []PlanItem{trustItem("a", "H.md#1"), trustItem("b", "H.md#2")})
	if err != nil || len(a) != 2 || a[0].Spans[0].String() != "H.md:2-2" || a[1].Spans[0].String() != "H.md:2-2" {
		t.Fatalf("anchored = %+v err=%v", a, err)
	}
}
