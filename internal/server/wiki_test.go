package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeWiki writes a compiled wiki page into the backend tree at
// content/wiki/<rel>.md, creating parent dirs (e.g. modules/).
func writeWiki(t *testing.T, repoDir, rel, content string) {
	t.Helper()
	p := filepath.Join(repoDir, "content", "wiki", filepath.FromSlash(rel)+".md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", rel, err)
	}
}

type wikiPageRefJSON struct {
	Name string `json:"name"`
	Rel  string `json:"rel"`
}

// TestWikiIndexListsCompiledPages: GET /api/wiki lists the pages that exist on
// disk, including nested module pages, with canonical names.
func TestWikiIndexListsCompiledPages(t *testing.T) {
	repoDir, projDir := newBackend(t)
	writeWiki(t, repoDir, "project", "---\ncerne_wiki: project\n---\n\n# Projeto\n\nSíntese geral.")
	writeWiki(t, repoDir, "modules/core", "---\ncerne_wiki: core\n---\n\n# core\n\nMódulo core.")

	var got struct {
		Pages []wikiPageRefJSON `json:"pages"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/wiki", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	byRel := map[string]string{}
	for _, p := range got.Pages {
		byRel[p.Rel] = p.Name
	}
	if byRel["project"] != "project" {
		t.Fatalf("pages missing project: %+v", got.Pages)
	}
	if byRel["modules/core"] != "core" {
		t.Fatalf("pages missing modules/core (name=core): %+v", got.Pages)
	}
}

// TestWikiIndexEmpty: a backend with no compiled wiki returns an empty list,
// not an error.
func TestWikiIndexEmpty(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got struct {
		Pages []wikiPageRefJSON `json:"pages"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/wiki", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(got.Pages) != 0 {
		t.Fatalf("pages = %+v, want empty", got.Pages)
	}
}

// TestWikiPageReturnsBodyWithoutFrontmatter: GET /api/wiki/<rel> returns the
// synthesis body with the provenance frontmatter stripped.
func TestWikiPageReturnsBodyWithoutFrontmatter(t *testing.T) {
	repoDir, projDir := newBackend(t)
	writeWiki(t, repoDir, "project", "---\ncerne_wiki: project\nsources: [F-0001]\n---\n\n# Projeto\n\nCorpo da síntese.")
	writeWiki(t, repoDir, "modules/core", "---\ncerne_wiki: core\n---\n\n# core\n\nMódulo core.")

	var got struct {
		Rel  string `json:"rel"`
		Body string `json:"body"`
	}
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/wiki/project", "", &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if got.Rel != "project" {
		t.Fatalf("rel = %q, want project", got.Rel)
	}
	if !strings.Contains(got.Body, "# Projeto") || !strings.Contains(got.Body, "Corpo da síntese") {
		t.Fatalf("body missing synthesis content: %q", got.Body)
	}
	if strings.Contains(got.Body, "cerne_wiki") || strings.Contains(got.Body, "sources:") {
		t.Fatalf("body leaked provenance frontmatter: %q", got.Body)
	}

	// nested module page resolves too
	rec = doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/wiki/modules/core", "", &got)
	if rec.Code != http.StatusOK || !strings.Contains(got.Body, "Módulo core") {
		t.Fatalf("modules/core: status=%d body=%q", rec.Code, got.Body)
	}
}

// TestWikiPageNotFound: an unknown page is a 404 with a pt-BR message.
func TestWikiPageNotFound(t *testing.T) {
	repoDir, projDir := newBackend(t)
	var got map[string]string
	rec := doJSON(t, New(repoDir, projDir, ""), http.MethodGet, "/api/wiki/ghost", "", &got)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", rec.Code, rec.Body.String())
	}
	if got["error"] == "" {
		t.Fatalf("body = %v, want pt-BR error message", got)
	}
}

// TestSafeWikiRel guards path traversal: the security boundary for serving wiki
// pages by relative path. Tested directly so it does not depend on how the mux
// canonicalizes escaped URLs.
func TestSafeWikiRel(t *testing.T) {
	valid := []string{"project", "decisions", "references", "modules/core", "modules/web-app"}
	for _, rel := range valid {
		if !safeWikiRel(rel) {
			t.Errorf("safeWikiRel(%q) = false, want true", rel)
		}
	}
	invalid := []string{"", "..", "../etc", "modules/../secret", "/etc/passwd", "modules/..%2Fx", `modules\core`, ".hidden", "a/../b"}
	for _, rel := range invalid {
		if safeWikiRel(rel) {
			t.Errorf("safeWikiRel(%q) = true, want false", rel)
		}
	}
}
