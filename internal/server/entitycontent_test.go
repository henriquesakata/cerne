package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestDetailContentHash (F-0464 crit. 6): the reads the vault editor loads
// from carry the hash a write sends back as If-Match — content_hash on a
// decision, note and runbook, docs_hash per document on a feature, the hash
// of "" for an absent document; a reference carries none.
func TestDetailContentHash(t *testing.T) {
	repoDir, projDir := newBackend(t)
	now := time.Now().UTC()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "D", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}))
	must(store.SaveNote(repoDir, store.Note{ID: "N-0001", Title: "N", State: domain.NoteActive, CreatedAt: now, UpdatedAt: now}))
	must(store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0001", Title: "RB", State: domain.RunbookActive, CreatedAt: now, UpdatedAt: now}))
	must(store.SaveReference(repoDir, store.Reference{ID: "R-0001", Title: "R", State: domain.ReferenceCurrent, CreatedAt: now, UpdatedAt: now}))
	writeContentDoc(t, repoDir, "decisions", "D-0001", "# Decisão\n\nCorpo.\n")
	writeContentDoc(t, repoDir, "notes", "N-0001", "# Nota\n\nCorpo.\n")
	writeContentDoc(t, repoDir, "references", "R-0001", "# Referência\n")
	// RB-0001 has no content document: its hash is the hash of "".
	seedFeature(t, repoDir, "F-0001", "Feature", domain.StateCandidate)
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Problema."))
	h := New(repoDir, projDir, "")
	hashOf := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(rel)))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return app.ContentHash(b)
	}

	for _, c := range []struct{ url, rel string }{
		{"/api/decisions/D-0001", "content/decisions/D-0001.md"},
		{"/api/notes/N-0001", "content/notes/N-0001.md"},
		{"/api/runbooks/RB-0001", "content/runbooks/RB-0001.md"},
	} {
		var got map[string]any
		if rec := doJSON(t, h, http.MethodGet, c.url, "", &got); rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", c.url, rec.Code)
		}
		if got["content_hash"] != hashOf(c.rel) {
			t.Errorf("GET %s content_hash = %v, want %s", c.url, got["content_hash"], hashOf(c.rel))
		}
	}
	if hashOf("content/runbooks/RB-0001.md") != app.ContentHash(nil) {
		t.Fatalf("fixture: RB-0001 must have no content document")
	}

	var feat struct {
		DocsHash map[string]string `json:"docs_hash"`
	}
	if rec := doJSON(t, h, http.MethodGet, "/api/features/F-0001", "", &feat); rec.Code != http.StatusOK {
		t.Fatalf("GET feature = %d", rec.Code)
	}
	want := map[string]string{
		"spec":   hashOf("content/features/F-0001/spec.md"),
		"plan":   app.ContentHash(nil),
		"result": app.ContentHash(nil),
	}
	for doc, h := range want {
		if feat.DocsHash[doc] != h {
			t.Errorf("docs_hash[%s] = %q, want %q", doc, feat.DocsHash[doc], h)
		}
	}

	var ref map[string]any
	if rec := doJSON(t, h, http.MethodGet, "/api/references/R-0001", "", &ref); rec.Code != http.StatusOK {
		t.Fatalf("GET reference = %d", rec.Code)
	}
	if _, has := ref["content_hash"]; has {
		t.Errorf("reference carries content_hash: %v", ref["content_hash"])
	}
}
