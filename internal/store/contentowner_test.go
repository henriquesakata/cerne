package store

import (
	"path/filepath"
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

// TestEntityIDFromContentPathEveryKind (B-0022): for every kind in the layout
// table, the content dir that really holds its docs maps back to its id. The
// dirs are the ones the writers use (DocPath helpers in app, the handoff and
// worklog paths in store); a kind that has no content dir says so here, so a
// new kind in idSubdirs fails this test until someone decides which it is.
func TestEntityIDFromContentPathEveryKind(t *testing.T) {
	// prefix -> a real content doc path of that kind, relative to the backend.
	holds := map[string]string{
		"F":  "content/features/F-0001/spec.md",
		"E":  "content/features/E-0001/spec.md",
		"T":  "content/features/T-0001/spec.md",
		"B":  "content/features/B-0001/spec.md",
		"D":  "content/decisions/D-0001.md",
		"R":  "content/references/R-0001.md",
		"RB": "content/runbooks/RB-0001.md",
		"N":  "content/notes/N-0001.md",
		"HO": "content/handoffs/HO-0001.md",
		"W":  "content/works/W-0001/handoff.md",
	}
	// Kinds whose docs live in no content dir: entity JSON only.
	noContent := map[string]string{"H": "historical", "C": "capability", "P": "pending", "I": "idea"}
	for prefix := range idSubdirs {
		rel, ok := holds[prefix]
		if !ok {
			if _, none := noContent[prefix]; !none {
				t.Errorf("kind %s is in idSubdirs but in neither table: say where its content docs live", prefix)
			}
			continue
		}
		wantID := prefix + "-0001"
		if got := EntityIDFromContentPath(rel); got != wantID {
			t.Errorf("%s: EntityIDFromContentPath(%q) = %q, want %q", prefix, rel, got, wantID)
		}
	}
	// The writers' own paths agree with the table.
	dir := "/b"
	if rel, _ := filepath.Rel(dir, handoffContentPath(dir, "HO-0001")); filepath.ToSlash(rel) != holds["HO"] {
		t.Errorf("handoff writer path %q != table %q", rel, holds["HO"])
	}
	if rel, _ := filepath.Rel(dir, handoffPath(dir, "W-0001")); filepath.ToSlash(rel) != holds["W"] {
		t.Errorf("worklog path %q != table %q", rel, holds["W"])
	}
}
