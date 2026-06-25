package importer

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTree materializes rel-path -> content under a fresh temp dir and returns
// its root. Parent directories are created as needed.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func relPaths(sources []Source) []string {
	out := make([]string, len(sources))
	for i, s := range sources {
		out[i] = s.RelPath
	}
	return out
}

func TestCollectSourcesGenericTree(t *testing.T) {
	root := writeTree(t, map[string]string{
		"CLAUDE.md":   "# Estado atual\nprojeto X",
		"HISTORY.md":  "## 1.2.0\n- mudou Y",
		"docs/use.md": "# Como usar",
	})
	got, err := CollectSources(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"CLAUDE.md", "HISTORY.md", "docs/use.md"}
	if diff := relPaths(got); !equalStrings(diff, want) {
		t.Fatalf("rel paths = %v, want %v", diff, want)
	}
	for _, s := range got {
		if s.Content == "" {
			t.Fatalf("source %s has empty content", s.RelPath)
		}
	}
}

func TestCollectSourcesHubTree(t *testing.T) {
	root := writeTree(t, map[string]string{
		"docs/process/cases/0001_x/spec.md": "# Caso 1",
		"docs/architecture/adr/adr-001.md":  "# ADR 1",
	})
	got, err := CollectSources(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sources, want 2 (%v)", len(got), relPaths(got))
	}
}

func TestCollectSourcesIgnores(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.md":              "keep",
		"node_modules/b.md": "drop",
		".git/c.md":         "drop",
		".docs/d.md":        "drop",
		"vendor/e.md":       "drop",
		"dist/f.md":         "drop",
		"notes.txt":         "drop (not a doc source)",
	})
	got, err := CollectSources(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.md"}; !equalStrings(relPaths(got), want) {
		t.Fatalf("rel paths = %v, want %v", relPaths(got), want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
