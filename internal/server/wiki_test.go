package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/compile"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/wiki"
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

// staleWikiBackend is a backend whose module core has one live feature and a
// compiled page stamped with an old input hash — dirty, as when an entity
// changed after the last compile — plus a helper that rewrites the page with
// the current hash (compiled and fresh).
func staleWikiBackend(t *testing.T) (repoDir, projDir string, writePage func(fresh bool)) {
	t.Helper()
	repoDir, projDir = newBackend(t)
	now := time.Now().UTC()
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Motor", Module: "core", State: domain.StateApproved, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	writeDoc(t, repoDir, "F-0001", "spec.md", realDoc("F-0001", "Motor de cobrança."))
	writePage = func(fresh bool) {
		t.Helper()
		hash := "old"
		if fresh {
			pages, err := compile.Pages(repoDir)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range pages {
				if p.Rel == "modules/core" {
					hash = p.InputHash()
				}
			}
		}
		writeWiki(t, repoDir, "modules/core", wiki.RenderProvenance(wiki.Provenance{Page: "core", Sources: []string{"F-0001"}, CompiledFrom: "x", InputHash: hash})+"\n# core\n\nSíntese do core.\n")
	}
	writePage(false)
	return repoDir, projDir, writePage
}

type wikiIndexJSON struct {
	Pages         []wikiPageRefJSON `json:"pages"`
	Stale         []string          `json:"stale"`
	NeedsProvider bool              `json:"needs_provider"`
}

// TestWikiIndexStaleWhenWikiOff pins F-0316 criteria 6, 8 and 9 on the index:
// with the wiki off it names the pages compiled before their sources changed
// (and whether the browser can recompile them), names none once they are
// fresh, still lists and serves every page, and says nothing with the wiki on.
func TestWikiIndexStaleWhenWikiOff(t *testing.T) {
	repoDir, projDir, writePage := staleWikiBackend(t)
	rc := filepath.Join(projDir, ".cernerc")
	if err := os.WriteFile(rc, []byte("[wiki]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := New(repoDir, projDir, "")
	index := func() wikiIndexJSON {
		t.Helper()
		var got wikiIndexJSON
		if rec := doJSON(t, h, http.MethodGet, "/api/wiki", "", &got); rec.Code != http.StatusOK {
			t.Fatalf("GET /api/wiki: %d %s", rec.Code, rec.Body.String())
		}
		return got
	}

	got := index()
	if !slices.Equal(got.Stale, []string{"modules/core"}) || !got.NeedsProvider {
		t.Fatalf("wiki off, page dirty: stale = %v needs_provider = %v, want [modules/core] true (delegation)", got.Stale, got.NeedsProvider)
	}
	if len(got.Pages) != 1 || got.Pages[0].Rel != "modules/core" {
		t.Fatalf("wiki off: pages = %+v, want modules/core still listed", got.Pages)
	}
	var page struct {
		Body string `json:"body"`
	}
	if rec := doJSON(t, h, http.MethodGet, "/api/wiki/modules/core", "", &page); rec.Code != http.StatusOK || !strings.Contains(page.Body, "Síntese do core.") {
		t.Fatalf("wiki off: GET page = %d %q, want the compiled body", rec.Code, page.Body)
	}

	writePage(true)
	if got := index(); len(got.Stale) != 0 || got.NeedsProvider {
		t.Fatalf("wiki off, page fresh: stale = %v needs_provider = %v, want none", got.Stale, got.NeedsProvider)
	}

	writePage(false)
	if err := os.Remove(rc); err != nil {
		t.Fatal(err)
	}
	if got := index(); len(got.Stale) != 0 {
		t.Fatalf("wiki on (default), page dirty: stale = %v, want none — compile_on_push owns freshness", got.Stale)
	}
}

// TestWikiCompileEndpoint pins F-0316 criteria 7 and 10 on the server: with a
// callable provider POST /api/wiki/compile runs the compile for the project to
// completion and reports it, a failed run comes back as an error naming the
// cause, delegation runs nothing and asks for a provider, and the remote serve
// never mounts the route.
func TestWikiCompileEndpoint(t *testing.T) {
	var ran []string
	var fail bool
	orig := wikiCompileRunner
	wikiCompileRunner = func(projDir string) ([]byte, error) {
		ran = append(ran, projDir)
		if fail {
			return []byte("Compiling 1 page(s)…\nError: boom\n\n"), errors.New("exit status 1")
		}
		return []byte("Compiled 1 page(s).\n"), nil
	}
	t.Cleanup(func() { wikiCompileRunner = orig })

	repoDir, projDir := newBackend(t)
	if err := project.SaveConfig(projDir, project.Config{
		ProjectID: "proj-wiki",
		Backend:   "unused",
		LLM:       project.LLMConfig{Mode: "byo-key", BaseURL: "http://127.0.0.1:1", KeyEnv: "TEST_WIKI_KEY"},
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	h := New(repoDir, projDir, "")

	var ok struct {
		Compiled      bool `json:"compiled"`
		NeedsProvider bool `json:"needs_provider"`
	}
	if rec := doJSON(t, h, http.MethodPost, "/api/wiki/compile", "", &ok); rec.Code != http.StatusOK || !ok.Compiled || ok.NeedsProvider {
		t.Fatalf("POST /api/wiki/compile = %d %s, want 200 compiled", rec.Code, rec.Body.String())
	}
	if !slices.Equal(ran, []string{projDir}) {
		t.Fatalf("runner calls = %v, want one for %s", ran, projDir)
	}

	fail = true
	var failed map[string]string
	if rec := doJSON(t, h, http.MethodPost, "/api/wiki/compile", "", &failed); rec.Code != http.StatusInternalServerError || !strings.Contains(failed["error"], "boom") {
		t.Fatalf("failed compile = %d %s, want 500 naming boom", rec.Code, rec.Body.String())
	}

	ran = nil
	delegRepo, delegProj := newBackend(t)
	var deleg struct {
		Compiled      bool `json:"compiled"`
		NeedsProvider bool `json:"needs_provider"`
	}
	if rec := doJSON(t, New(delegRepo, delegProj, ""), http.MethodPost, "/api/wiki/compile", "", &deleg); rec.Code != http.StatusOK || !deleg.NeedsProvider || deleg.Compiled {
		t.Fatalf("delegation = %d %s, want 200 needs_provider", rec.Code, rec.Body.String())
	}

	mux := http.NewServeMux()
	mountDataRoutes(mux, "", func(sh scopedHandler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { sh(w, r, repoDir, projDir, "") }
	}, true)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/wiki/compile", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("remote subset served POST /api/wiki/compile: %d %s", rec.Code, rec.Body.String())
	}
	if len(ran) != 0 {
		t.Fatalf("runner ran %v without a callable provider or on the remote serve", ran)
	}
}
