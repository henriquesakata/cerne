package store

import (
	"slices"
	"testing"
)

// TestEntityIDFromContentPath (B-0022): the owner of a content doc is read off
// the layout table, so every kind with a content dir resolves, not only
// features, decisions and references.
func TestEntityIDFromContentPath(t *testing.T) {
	for rel, want := range map[string]string{
		"content/features/F-0001/spec.md":  "F-0001",
		"content/features/B-0012/spec.md":  "B-0012",
		"content/decisions/D-0001.md":      "D-0001",
		"content/references/R-0002.md":     "R-0002",
		"content/notes/N-0003.md":          "N-0003",
		"content/runbooks/RB-0004.md":      "RB-0004",
		"content/handoffs/HO-0005.md":      "HO-0005",
		"content/works/W-0006/handoff.md":  "W-0006",
		"content/notes/N-0003/x.md":        "N-0003", // a dir under a one-file kind still names its owner
		"content/notes/D-0001.md":          "",       // the id's kind must own the dir
		"content/notes/N-0003.txt":         "",
		"content/wiki/page.md":             "",
		"content/artifacts/F-0001/x.md":    "",
		"entities/notes/N-0003.json":       "",
		"content/features/../notes/N-1.md": "",
	} {
		if got := EntityIDFromContentPath(rel); got != want {
			t.Errorf("EntityIDFromContentPath(%q) = %q, want %q", rel, got, want)
		}
	}
}

// TestEntityIDs: every entity file of every kind in the layout table, by id.
func TestEntityIDs(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"F-0001", "W-0002", "C-0003", "HO-0004", "N-0005"} {
		if err := saveEntity(dir, idSubdirs[idPrefix(id)], id, map[string]string{"id": id}); err != nil {
			t.Fatal(err)
		}
	}
	got := EntityIDs(dir)
	slices.Sort(got)
	if want := []string{"C-0003", "F-0001", "HO-0004", "N-0005", "W-0002"}; !slices.Equal(got, want) {
		t.Fatalf("EntityIDs = %v, want %v", got, want)
	}
}

func idPrefix(id string) string {
	for i := range id {
		if id[i] == '-' {
			return id[:i]
		}
	}
	return id
}
