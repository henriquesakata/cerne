package e2e_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// TestNoteGoldenPath walks the note loop end to end: init → note new (N-0001
// born active, scaffolded as a stub) → pull (materialized, listed in CONTEXT.md's
// Conhecimento section) → write the real note in .docs/ → push → ask (cites the
// note from the index) → note new (successor) → note substituir → pull (gone from
// .docs/, kept in searchable history), asserting the .docs/ projection and the
// backend via a fresh clone of origin.
func TestNoteGoldenPath(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj := t.TempDir()
	docsDir := filepath.Join(proj, ".docs")
	ctxPath := filepath.Join(docsDir, "CONTEXT.md")
	notePath := filepath.Join(docsDir, "current", "notes", "N-0001-go-git-ignora-ssh-config.md")

	mustCerne(t, "init", "--docs-language", "pt-BR", "--name", "Demo", "--backend", origin, "--module", "arch", "--dir", proj)

	out := mustCerne(t, "note", "new", "go-git ignora ssh config", "--dir", proj)
	if !strings.Contains(out, "N-0001 created (active)") {
		t.Fatalf("note new output missing N-0001 confirmation:\n%s", out)
	}

	mustCerne(t, "pull", "--dir", proj)
	stub := mustRead(t, notePath)
	const frontmatter = "---\ncerne_id: N-0001\n---\n"
	if !strings.HasPrefix(stub, frontmatter) {
		t.Fatalf("materialized note does not start with the expected frontmatter:\n%s", stub)
	}
	ctx := mustRead(t, ctxPath)
	if !strings.Contains(ctx, "## Knowledge") || !strings.Contains(ctx, "N-0001 — go-git ignora ssh config") {
		t.Fatalf("CONTEXT.md missing active note N-0001 under Knowledge:\n%s", ctx)
	}

	realNote := frontmatter + "\n# go-git ignora ssh config\n\n" +
		"O go-git não respeita ~/.ssh/config; use o fallback de key-file explícito.\n"
	if err := os.WriteFile(notePath, []byte(realNote), 0o644); err != nil {
		t.Fatalf("write real note: %v", err)
	}
	mustCerne(t, "push", "--dir", proj)
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("Clone origin for verification: %v", err)
	}
	pushed := mustRead(t, filepath.Join(verify, "content", "notes", "N-0001.md"))
	if pushed != realNote {
		t.Fatalf("origin note = %q, want the pushed content verbatim", pushed)
	}

	// Re-sync after the push (pull rebuilds the local FTS index from the backend),
	// then ask: in delegation mode (default) it prints the assembled prompt
	// embedding the retrieved sources tagged by state — the active note must be
	// cited among them.
	mustCerne(t, "pull", "--dir", proj)
	answer := mustCerne(t, "ask", "como o go-git lida com ssh config?", "--dir", proj)
	if !strings.Contains(answer, "N-0001 (note, active)") {
		t.Fatalf("ask did not cite the active note N-0001 from the index:\n%s", answer)
	}

	out = mustCerne(t, "note", "new", "Nota sucessora", "--dir", proj)
	if !strings.Contains(out, "N-0002 created (active)") {
		t.Fatalf("second note new output missing N-0002 confirmation:\n%s", out)
	}
	out = mustCerne(t, "note", "substituir", "N-0001", "--by", "N-0002", "--reason", "corrigida", "--dir", proj)
	if !strings.Contains(out, "active") || !strings.Contains(out, "superseded") {
		t.Fatalf("substituir output missing old→new state:\n%s", out)
	}
	mustCerne(t, "pull", "--dir", proj)
	if _, err := os.Stat(notePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("superseded note still materialized (stat err = %v)", err)
	}
	ctx = mustRead(t, ctxPath)
	if strings.Contains(ctx, "N-0001") {
		t.Fatalf("CONTEXT.md still mentions superseded N-0001:\n%s", ctx)
	}
	if !strings.Contains(ctx, "N-0002 — Nota sucessora") {
		t.Fatalf("CONTEXT.md missing successor N-0002:\n%s", ctx)
	}

	// The superseded note remains searchable history (index covers all states).
	hist := mustCerne(t, "search", "key-file", "--dir", proj)
	if !strings.Contains(hist, "N-0001") {
		t.Fatalf("search did not surface superseded note N-0001 in history:\n%s", hist)
	}
}
