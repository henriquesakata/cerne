package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestReferenceNew registers an external source via the CLI: content from a
// file, linked to a feature via --informs; the entity persists external/url/informs.
func TestReferenceNew(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "feature", "new", "Corrige login", "--context", "ctx de teste", "--dir", projDir); err != nil {
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
	if !strings.Contains(out, "R-0001 registered") || !strings.Contains(out, "example.com/pkce") {
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

// TestReferenceNewWithTags: --facet/--component are normalized (lower/slug) and
// persisted on the reference, mirroring note new.
func TestReferenceNewWithTags(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	contentFile := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(contentFile, []byte("# SSH\n\nknown_hosts."), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := execCerne(t, "reference", "new", "Artigo SSH no Gitea",
		"--url", "https://example.com/ssh", "--facet", "Infra,code",
		"--component", "Gitea,KPack", "--file", contentFile, "--dir", projDir)
	if err != nil {
		t.Fatalf("reference new: %v\n%s", err, out)
	}
	r, err := store.LoadReference(repoDir, "R-0001")
	if err != nil {
		t.Fatalf("LoadReference: %v", err)
	}
	if len(r.Facets) != 2 || r.Facets[0] != "infra" || r.Facets[1] != "code" {
		t.Fatalf("facets = %v, want [infra code]", r.Facets)
	}
	if len(r.Components) != 2 || r.Components[0] != "gitea" || r.Components[1] != "kpack" {
		t.Fatalf("components = %v, want [gitea kpack]", r.Components)
	}
}

// TestReferenceList: lists current references, filtered by facet/component (AND
// between axes); --all includes obsolete; --json emits structured output.
func TestReferenceList(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	mk := func(title, url, facet, component string) {
		cf := filepath.Join(t.TempDir(), "c.md")
		if err := os.WriteFile(cf, []byte("corpo"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		args := []string{"reference", "new", title, "--url", url, "--file", cf, "--dir", projDir}
		if facet != "" {
			args = append(args, "--facet", facet)
		}
		if component != "" {
			args = append(args, "--component", component)
		}
		if out, err := execCerne(t, args...); err != nil {
			t.Fatalf("reference new %s: %v\n%s", title, err, out)
		}
	}
	mk("Infra source", "http://a", "infra", "gitea")
	mk("Code source", "http://b", "code", "")
	// An obsolete reference (no CLI path to it) — saved directly to assert --all.
	if err := store.SaveReference(repoDir, store.Reference{
		ID: "R-0003", Title: "Velha", State: domain.ReferenceObsolete,
	}); err != nil {
		t.Fatalf("SaveReference: %v", err)
	}

	// Unfiltered: both current refs, not the obsolete one.
	out, err := execCerne(t, "reference", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "R-0001") || !strings.Contains(out, "R-0002") || strings.Contains(out, "R-0003") {
		t.Fatalf("default list should show current only:\n%s", out)
	}

	// --all includes the obsolete one.
	if out, _ = execCerne(t, "reference", "list", "--all", "--dir", projDir); !strings.Contains(out, "R-0003") {
		t.Fatalf("--all should include obsolete:\n%s", out)
	}

	// Filter by facet infra → only R-0001.
	if out, _ = execCerne(t, "reference", "list", "--facet", "infra", "--dir", projDir); !strings.Contains(out, "R-0001") || strings.Contains(out, "R-0002") {
		t.Fatalf("facet filter wrong:\n%s", out)
	}

	// Filter by component gitea → only R-0001.
	if out, _ = execCerne(t, "reference", "list", "--component", "gitea", "--dir", projDir); !strings.Contains(out, "R-0001") || strings.Contains(out, "R-0002") {
		t.Fatalf("component filter wrong:\n%s", out)
	}

	// JSON output carries the structured entity.
	if out, err = execCerne(t, "reference", "list", "--json", "--dir", projDir); err != nil || !strings.Contains(out, "\"id\": \"R-0001\"") {
		t.Fatalf("json list = %q, %v", out, err)
	}
}

// F-0389 crit. 2/3: reference link adds, --unlink removes, prints the final list.
func TestReferenceLink(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if out, err := execCerne(t, "idea", "new", "uma ideia", "--dir", projDir); err != nil {
		t.Fatalf("idea new: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "reference", "new", "Fonte", "--url", "http://x", "--file", "/dev/null", "--dir", projDir); err != nil {
		t.Fatalf("reference new: %v\n%s", err, out)
	}
	out, err := execCerne(t, "reference", "link", "R-0001", "--informs", "I-0001", "--dir", projDir)
	if err != nil || !strings.HasPrefix(out, "R-0001 informs: I-0001\n") {
		t.Fatalf("link: err=%v out=%q", err, out)
	}
	if r, _ := store.LoadReference(repoDir, "R-0001"); len(r.Informs) != 1 || r.Informs[0] != "I-0001" {
		t.Fatalf("persisted = %+v", r.Informs)
	}
	out, err = execCerne(t, "reference", "link", "R-0001", "--unlink", "I-0001", "--dir", projDir)
	if err != nil || !strings.HasPrefix(out, "R-0001 informs: (none)\n") {
		t.Fatalf("unlink: err=%v out=%q", err, out)
	}
	if _, err := execCerne(t, "reference", "link", "R-0001", "--dir", projDir); err == nil {
		t.Fatal("no flags must be refused")
	}
	if _, err := execCerne(t, "reference", "link", "R-0001", "--informs", "I-9999", "--dir", projDir); err == nil {
		t.Fatal("dangling idea must be refused")
	}
	_, stderr, err := execCerneSplit(t, "reference", "link", "R-0001", "--unlink", "F-0042", "--dir", projDir)
	if err != nil || !strings.Contains(stderr, "not present: F-0042") {
		t.Fatalf("unlink of an absent target: err=%v stderr=%q", err, stderr)
	}
	if _, _, err := execCerneSplit(t, "reference", "link", "R-0001", "--informs", "I-0001", "--unlink", "I-0001", "--dir", projDir); err == nil {
		t.Fatal("an id in both lists must be refused")
	}
}
