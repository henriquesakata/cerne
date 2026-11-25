package index

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// B-0075: the indexed body has no frontmatter and no HTML comment (the
// template's guidance lines), so a search snippet quotes the document, never
// "--- … <!-- HOW TO USE: …". Every readDoc caller gets it: features and notes
// alike.
func TestSearchSkipsFrontmatterAndTemplateComments(t *testing.T) {
	repoDir := t.TempDir()
	now := time.Now().UTC()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0002", Title: "Sign in with GitHub", State: domain.StateCandidate, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveNote(repoDir, store.Note{ID: "N-0001", Title: "OAuth note", State: domain.NoteActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	writeContent(t, repoDir, "content/features/F-0002/spec.md",
		"---\ncerne_id: F-0002\n---\n\n# Sign in with GitHub\n\n<!-- HOW TO USE: fill in every section with the github placeholder -->\n\n## Problem\n\nUsers log in with GitHub accounts.\n")
	writeContent(t, repoDir, "content/notes/N-0001.md",
		"---\ncerne_id: N-0001\n---\n\n<!-- guidance: oauthguide -->\nThe callback runs on oauth.\n")
	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := Rebuild(repoDir, dbPath, time.Time{}); err != nil {
		t.Fatal(err)
	}
	res, err := Search(dbPath, "github", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.EntityID == "F-0002" && r.Doc == "spec" {
			if strings.Contains(r.Snippet, "---") || strings.Contains(r.Snippet, "<!--") || strings.Contains(r.Snippet, "HOW TO USE") {
				t.Errorf("snippet quotes the scaffold: %q", r.Snippet)
			}
		}
	}
	for _, q := range []string{"oauthguide", "placeholder"} {
		if res, _ := Search(dbPath, q, 10); len(res) != 0 {
			t.Errorf("%q (only inside a comment) matched %+v", q, res)
		}
	}
	if res, _ := Search(dbPath, "callback", 10); len(res) != 1 || res[0].EntityID != "N-0001" {
		t.Errorf("note body lost with its comment: %+v", res)
	}
}
