package e2e_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// TestNoteTagsGoldenPath: notes created with --facet/--component are filtered by
// `note list` (AND not needed here — one axis each), surface their tags in
// CONTEXT.md, and are findable by a tag search even when the term is absent from
// the note's title and body.
func TestNoteTagsGoldenPath(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj := t.TempDir()
	mustCerne(t, "init", "--docs-language", "pt-BR", "--name", "Demo", "--backend", origin, "--module", "arch", "--dir", proj)

	bodyFile := func(text string) string {
		p := filepath.Join(t.TempDir(), "body.md")
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatalf("write body: %v", err)
		}
		return p
	}

	// "gitea" appears in neither title nor body — only in the component tag.
	mustCerne(t, "note", "new", "Instalação do registry interno",
		"--file", bodyFile("Procedimento de setup do ambiente.\n"),
		"--facet", "infra", "--component", "Gitea", "--dir", proj) // N-0001
	mustCerne(t, "note", "new", "Refactor do checkout",
		"--file", bodyFile("Notas sobre o fluxo de código.\n"),
		"--facet", "code", "--dir", proj) // N-0002

	byFacet := mustCerne(t, "note", "list", "--facet", "infra", "--dir", proj)
	if !strings.Contains(byFacet, "N-0001") || strings.Contains(byFacet, "N-0002") {
		t.Fatalf("list --facet infra = %q, want only N-0001", byFacet)
	}
	if !strings.Contains(byFacet, "facet infra") || !strings.Contains(byFacet, "comp gitea") {
		t.Fatalf("list line missing inline tags:\n%s", byFacet)
	}

	byComp := mustCerne(t, "note", "list", "--component", "gitea", "--dir", proj)
	if !strings.Contains(byComp, "N-0001") || strings.Contains(byComp, "N-0002") {
		t.Fatalf("list --component gitea = %q, want only N-0001", byComp)
	}

	// CONTEXT.md surfaces the tags after a pull.
	mustCerne(t, "pull", "--dir", proj)
	ctx := mustRead(t, filepath.Join(proj, ".docs", "CONTEXT.md"))
	if !strings.Contains(ctx, "facet infra") || !strings.Contains(ctx, "comp gitea") {
		t.Fatalf("CONTEXT.md missing note tags:\n%s", ctx)
	}

	// The tag is searchable though "gitea" is in neither title nor body.
	hits := mustCerne(t, "search", "gitea", "--dir", proj)
	if !strings.Contains(hits, "N-0001") {
		t.Fatalf("search gitea did not surface N-0001 via component tag:\n%s", hits)
	}
}
