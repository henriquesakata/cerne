package materialize

import (
	"os"
	"path/filepath"
	"testing"
)

func writeWiki(t *testing.T, backendDir, rel, body string) {
	t.Helper()
	p := filepath.Join(backendDir, "content", "wiki", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write wiki %s: %v", rel, err)
	}
}

func TestMaterializeProjectsWiki(t *testing.T) {
	backend := seedBackend(t)
	const finBody = "---\ncerne_wiki: finance\n---\n\n# Finance\n\nsíntese do finance.\n"
	const projBody = "---\ncerne_wiki: project\n---\n\n# Projeto\n\nvisão geral.\n"
	writeWiki(t, backend, "modules/finance.md", finBody)
	writeWiki(t, backend, "project.md", projBody)

	docsDir := filepath.Join(t.TempDir(), ".docs")
	manifest, err := Materialize(backend, docsDir, "", false)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(docsDir, "wiki", "modules", "finance.md"))
	if err != nil {
		t.Fatalf("read projected finance wiki: %v", err)
	}
	if string(got) != finBody {
		t.Fatalf("projected finance wiki not verbatim:\n%s", got)
	}
	if _, ok := manifest["wiki/modules/finance.md"]; !ok {
		t.Fatalf("manifest missing wiki/modules/finance.md: %v", manifest)
	}
	if _, ok := manifest["wiki/project.md"]; !ok {
		t.Fatalf("manifest missing wiki/project.md: %v", manifest)
	}
}

func TestMaterializeNoWiki(t *testing.T) {
	backend := seedBackend(t)
	docsDir := filepath.Join(t.TempDir(), ".docs")
	manifest, err := Materialize(backend, docsDir, "", false)
	if err != nil {
		t.Fatalf("Materialize without wiki: %v", err)
	}
	for k := range manifest {
		if len(k) >= 5 && k[:5] == "wiki/" {
			t.Fatalf("unexpected wiki entry %q with no compiled pages", k)
		}
	}
}
