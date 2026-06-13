package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/wiki"
)

// enableByoKey rewrites the project's .cernerc to byo-key mode pointing base_url
// at the given test server and key_env at keyEnv.
func enableByoKey(t *testing.T, projDir, baseURL, keyEnv string) {
	t.Helper()
	cfg, dir, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.LLM = project.LLMConfig{Mode: "byo-key", Provider: "anthropic", KeyEnv: keyEnv, BaseURL: baseURL}
	if err := project.SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

func TestCompileByoKeySynthesizesAndCommits(t *testing.T) {
	projDir, origin, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotPrompt = string(raw)
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"# Finance\n\nSíntese byo-key da página."}]}`)
	}))
	defer srv.Close()

	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	headBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}

	out, err := execCerne(t, "compile", "--force", "--dir", projDir)
	if err != nil {
		t.Fatalf("compile byo-key: %v\n%s", err, out)
	}

	// The provider was called with the deterministic page prompt.
	if !strings.Contains(gotPrompt, "Boletos") {
		t.Fatalf("provider prompt missing the feature title:\n%s", gotPrompt)
	}

	// The page was written to the backend with the synthesized body...
	pagePath := filepath.Join(repoDir, "content", "wiki", "modules", "finance.md")
	stamped, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatalf("read compiled page: %v", err)
	}
	if !strings.Contains(string(stamped), "Síntese byo-key da página.") {
		t.Fatalf("page missing the synthesized body:\n%s", stamped)
	}
	// ...and valid provenance frontmatter.
	prov := wiki.ParseProvenance(stamped)
	if prov.Page != "finance" || prov.InputHash == "" {
		t.Fatalf("page not stamped: %+v", prov)
	}
	if len(prov.Sources) != 1 || prov.Sources[0] != "F-0001" {
		t.Fatalf("provenance sources = %v, want [F-0001]", prov.Sources)
	}
	if prov.CompiledFrom == "" {
		t.Fatalf("provenance compiled_from empty")
	}

	// Exactly one commit advanced origin (push happened).
	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter == headBefore {
		t.Fatalf("origin HEAD did not advance after byo-key compile")
	}

	// No delegation artifacts in byo-key mode.
	if _, err := os.Stat(filepath.Join(projDir, ".docs", "wiki", ".pending")); err == nil {
		t.Fatalf(".pending dir created in byo-key mode")
	}

	// A pt-BR summary mentioning the byo-key mode.
	if !strings.Contains(out, "byo-key") {
		t.Fatalf("compile output missing byo-key summary:\n%s", out)
	}
}

// TestCompileByoKeyNothingDirty makes a clean compile a no-op: the provider is
// never called and origin does not advance.
func TestCompileByoKeyNothingDirty(t *testing.T) {
	projDir, origin, _ := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"x"}]}`)
	}))
	defer srv.Close()
	t.Setenv("TEST_BYO_KEY", "sk-byo")
	enableByoKey(t, projDir, srv.URL, "TEST_BYO_KEY")

	// First compile (force) writes the page.
	if out, err := execCerne(t, "compile", "--force", "--dir", projDir); err != nil {
		t.Fatalf("first compile: %v\n%s", err, out)
	}
	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	called = false

	// Second compile without force: nothing dirty, provider not called.
	out, err := execCerne(t, "compile", "--dir", projDir)
	if err != nil {
		t.Fatalf("second compile: %v\n%s", err, out)
	}
	if called {
		t.Fatalf("provider called on a clean compile")
	}
	if !strings.Contains(out, "Nada a recompilar") {
		t.Fatalf("expected nothing-dirty message:\n%s", out)
	}
	head2, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA 2: %v", err)
	}
	if head2 != headAfter {
		t.Fatalf("origin advanced on a clean compile")
	}
}
