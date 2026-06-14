package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/store"
)

// TestReferenceNew registers an external source via the CLI: content from a
// file, linked to a feature via --informs; the entity persists external/url/informs.
func TestReferenceNew(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Corrige login", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	contentFile := filepath.Join(t.TempDir(), "artigo.md")
	if err := os.WriteFile(contentFile, []byte("# PKCE\n\nO PKCE protege o code grant."), 0o644); err != nil {
		t.Fatalf("write content: %v", err)
	}

	out, err := execCerne(t, "reference", "new", "Artigo sobre PKCE",
		"--url", "https://example.com/pkce", "--informs", "F-0001",
		"--file", contentFile, "--dir", projDir)
	if err != nil {
		t.Fatalf("reference new: %v\n%s", err, out)
	}
	if !strings.Contains(out, "R-0001 registrada") || !strings.Contains(out, "example.com/pkce") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}

	r, err := store.LoadReference(repoDir, "R-0001")
	if err != nil {
		t.Fatalf("LoadReference: %v", err)
	}
	if !r.External || r.URL != "https://example.com/pkce" || len(r.Informs) != 1 || r.Informs[0] != "F-0001" {
		t.Fatalf("ref = %+v, want external, url, informs=[F-0001]", r)
	}
	doc, err := os.ReadFile(filepath.Join(repoDir, "content", "references", "R-0001.md"))
	if err != nil || !strings.Contains(string(doc), "O PKCE protege") {
		t.Fatalf("content doc = %q, %v; want the article text", string(doc), err)
	}
}

// TestReferenceNewDanglingInforms: linking to a non-existent target errors.
func TestReferenceNewDanglingInforms(t *testing.T) {
	projDir, _, _ := setupProject(t)
	contentFile := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(contentFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := execCerne(t, "reference", "new", "X", "--url", "http://x",
		"--informs", "F-9999", "--file", contentFile, "--dir", projDir)
	if err == nil {
		t.Fatalf("reference new with dangling --informs should fail")
	}
}

// TestReferenceNewRequiresURL: --url is mandatory.
func TestReferenceNewRequiresURL(t *testing.T) {
	projDir, _, _ := setupProject(t)
	_, err := execCerne(t, "reference", "new", "X", "--dir", projDir)
	if err == nil {
		t.Fatalf("reference new without --url should fail")
	}
}
