package server

import (
	"encoding/json"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// contentFixture is a git backend with an active and a superseded decision,
// note and runbook, each with a content document, plus F-0001 (candidate)
// with a spec — committed, so a write starts from them.
func contentFixture(t *testing.T) (repoDir, projDir string) {
	t.Helper()
	repoDir, projDir, _ = newGitBackend(t)
	now := time.Now().UTC()
	for _, err := range []error{
		store.SaveDecision(repoDir, store.Decision{ID: "D-0001", Title: "D", State: domain.DecisionActive, CreatedAt: now, UpdatedAt: now}),
		store.SaveDecision(repoDir, store.Decision{ID: "D-0002", Title: "D2", State: domain.DecisionSuperseded, CreatedAt: now, UpdatedAt: now}),
		store.SaveNote(repoDir, store.Note{ID: "N-0001", Title: "N", State: domain.NoteActive, CreatedAt: now, UpdatedAt: now}),
		store.SaveNote(repoDir, store.Note{ID: "N-0002", Title: "N2", State: domain.NoteSuperseded, CreatedAt: now, UpdatedAt: now}),
		store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0001", Title: "RB", State: domain.RunbookActive, CreatedAt: now, UpdatedAt: now}),
		store.SaveRunbook(repoDir, store.Runbook{ID: "RB-0002", Title: "RB2", State: domain.RunbookSuperseded, CreatedAt: now, UpdatedAt: now}),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct{ kind, id string }{
		{"decisions", "D-0001"}, {"decisions", "D-0002"}, {"notes", "N-0001"},
		{"notes", "N-0002"}, {"runbooks", "RB-0001"}, {"runbooks", "RB-0002"},
	} {
		writeContentDoc(t, repoDir, c.kind, c.id, "# "+c.id+"\n\nCorpo original.\n")
	}
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Problema original."))
	commitBackend(t, repoDir)
	return repoDir, projDir
}

// putContent sends a raw markdown PUT, with If-Match when ifMatch is not nil.
func putContent(t *testing.T, h http.Handler, target, body string, ifMatch *string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "text/markdown")
	if ifMatch != nil {
		req.Header.Set("If-Match", *ifMatch)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("PUT %s: invalid JSON %q: %v", target, rec.Body.String(), err)
	}
	return rec, out
}

// cloneTree is every file of the backend clone but .git, path → content.
func cloneTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[p] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// testPutContent is crit. 5 for one route: 200 writes the body as-is, 404 for
// an unknown id and for an id smuggling a path through %2F (B-0034) — nothing
// created or changed in the clone — and 422 with the errata message for a
// superseded entity.
func testPutContent(t *testing.T, kind, live, dead, unknown string) {
	repoDir, projDir := contentFixture(t)
	h := New(repoDir, projDir, "")
	base := "/api/" + kind + "s/"
	body := "# " + live + "\n\nCorpo corrigido.\n"

	rec, got := putContent(t, h, base+live+"/content", body, nil)
	if rec.Code != http.StatusOK || got["saved"] != true {
		t.Fatalf("PUT %s = %d %v, want 200 saved", live, rec.Code, got)
	}
	if b, _ := os.ReadFile(filepath.Join(repoDir, "content", kind+"s", live+".md")); string(b) != body {
		t.Fatalf("content = %q, want %q", b, body)
	}

	tree := cloneTree(t, repoDir)
	long := strings.Repeat("1", 5000) // audit A5: 404, not a 500 naming the clone path
	for _, id := range []string{unknown, "..%2F..%2Fentities%2Fnotes%2FN-0001", "..%2Fnotes%2FN-0001", "..%2F..%2F..%2Fescaped", live[:strings.Index(live, "-")+1] + long, live + "%00", live + "%5C..%5Cx"} {
		rec, got := putContent(t, h, base+id+"/content", "# Sobrescrito\n", nil)
		if rec.Code != http.StatusNotFound || got["error"] != kind+" not found" {
			t.Errorf("PUT %s = %d %v, want 404 %q", id, rec.Code, got, kind+" not found")
		}
	}
	if !maps.Equal(cloneTree(t, repoDir), tree) {
		t.Fatalf("a refused PUT created or changed a file in the clone")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(repoDir), "escaped.md")); err == nil {
		t.Fatalf("a refused PUT wrote outside the clone")
	}

	rec, got = putContent(t, h, base+dead+"/content", "# Reescrito\n", nil)
	if rec.Code != http.StatusUnprocessableEntity || got["error"] != "entity is not live — append an errata instead" {
		t.Fatalf("PUT %s = %d %v, want 422 with the errata message", dead, rec.Code, got)
	}
	if !maps.Equal(cloneTree(t, repoDir), tree) {
		t.Fatalf("a refused PUT on a superseded entity changed the clone")
	}
}

func TestPutDecisionContent(t *testing.T) {
	testPutContent(t, "decision", "D-0001", "D-0002", "D-0099")
}
func TestPutNoteContent(t *testing.T) { testPutContent(t, "note", "N-0001", "N-0002", "N-0099") }
func TestPutRunbookContent(t *testing.T) {
	testPutContent(t, "runbook", "RB-0001", "RB-0002", "RB-0099")
}

// TestPutContentIfMatch409 (F-0464 crit. 6): on the four write routes a stale
// If-Match is a 409 carrying the backend's content and its hash, with the
// document untouched; sending that hash back writes.
func TestPutContentIfMatch409(t *testing.T) {
	repoDir, projDir := contentFixture(t)
	h := New(repoDir, projDir, "")
	stale := app.ContentHash([]byte("a version this editor once loaded\n"))
	for _, c := range []struct{ url, rel string }{
		{"/api/decisions/D-0001/content", "content/decisions/D-0001.md"},
		{"/api/notes/N-0001/content", "content/notes/N-0001.md"},
		{"/api/runbooks/RB-0001/content", "content/runbooks/RB-0001.md"},
		{"/api/features/F-0001/docs/spec", "content/features/F-0001/spec.md"},
	} {
		t.Run(c.url, func(t *testing.T) {
			path := filepath.Join(repoDir, filepath.FromSlash(c.rel))
			cur, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			mine := string(cur) + "\nMinha linha.\n"
			rec, got := putContent(t, h, c.url, mine, &stale)
			if rec.Code != http.StatusConflict {
				t.Fatalf("stale If-Match = %d %v, want 409", rec.Code, got)
			}
			if got["error"] != app.ErrContentConflict.Error() || got["content"] != string(cur) || got["content_hash"] != app.ContentHash(cur) {
				t.Fatalf("409 body = %v, want the sentinel, the current content and its hash", got)
			}
			if after, _ := os.ReadFile(path); string(after) != string(cur) {
				t.Fatalf("a 409 changed the document")
			}
			hash := got["content_hash"].(string)
			if rec, got := putContent(t, h, c.url, mine, &hash); rec.Code != http.StatusOK || got["saved"] != true {
				t.Fatalf("If-Match from the 409 = %d %v, want 200 saved", rec.Code, got)
			}
			if after, _ := os.ReadFile(path); string(after) != mine {
				t.Fatalf("document = %q after the overwrite, want %q", after, mine)
			}
		})
	}
}

// TestPutFeatureDocWithoutIfMatch (F-0464 crit. 6): the feature page's editor
// sends no If-Match and writes as before; a blank one is no check either.
func TestPutFeatureDocWithoutIfMatch(t *testing.T) {
	repoDir, projDir := contentFixture(t)
	h := New(repoDir, projDir, "")
	blank := "  "
	for i, ifMatch := range []*string{nil, &blank} {
		body := realDoc("F-0001", "Problema reescrito "+string(rune('A'+i))+".")
		if rec, got := putContent(t, h, "/api/features/F-0001/docs/spec", body, ifMatch); rec.Code != http.StatusOK || got["saved"] != true {
			t.Fatalf("PUT without If-Match (%d) = %d %v, want 200 saved", i, rec.Code, got)
		}
		if b, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md")); string(b) != body {
			t.Fatalf("spec = %q, want %q", b, body)
		}
	}
}

// TestPutContentForeignCerneID (F-0464 audit A9): content whose frontmatter
// names another entity is a 400 on the content routes and the feature docs,
// with nothing written.
func TestPutContentForeignCerneID(t *testing.T) {
	repoDir, projDir := contentFixture(t)
	h := New(repoDir, projDir, "")
	tree := cloneTree(t, repoDir)
	for _, url := range []string{"/api/notes/N-0001/content", "/api/decisions/D-0001/content", "/api/runbooks/RB-0001/content", "/api/features/F-0001/docs/spec"} {
		rec, got := putContent(t, h, url, "---\ncerne_id: N-0999\n---\n\ncorpo\n", nil)
		if msg, _ := got["error"].(string); rec.Code != http.StatusBadRequest || !strings.Contains(msg, "cerne_id") {
			t.Errorf("PUT %s = %d %v, want 400 naming cerne_id", url, rec.Code, got)
		}
	}
	if !maps.Equal(cloneTree(t, repoDir), tree) {
		t.Fatalf("a refused PUT changed the clone")
	}
}
