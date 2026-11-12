package e2e_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// TestRunbookGoldenPath walks the runbook loop end to end: init → runbook new
// (RB-0001 born active, scaffolded from the template) → pull (materialized stub,
// listed in CONTEXT.md's Runbooks section) → write the real runbook in .docs/ →
// push → runbook new (successor) → runbook substituir → pull (gone from .docs/,
// kept in history), asserting the .docs/ projection and the backend via a fresh
// clone of origin.
func TestRunbookGoldenPath(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj := t.TempDir()
	docsDir := filepath.Join(proj, ".docs")
	ctxPath := filepath.Join(docsDir, "CONTEXT.md")
	rbPath := filepath.Join(docsDir, "current", "runbooks", "RB-0001-runbook-a.md")

	mustCerne(t, "init", "--docs-language", "pt-BR", "--name", "Demo", "--backend", origin, "--module", "arch", "--dir", proj)

	out := mustCerne(t, "runbook", "new", "Runbook A", "--dir", proj)
	if !strings.Contains(out, "RB-0001 created (active)") {
		t.Fatalf("runbook new output missing RB-0001 confirmation:\n%s", out)
	}

	mustCerne(t, "pull", "--dir", proj)
	stub := mustRead(t, rbPath)
	for _, want := range []string{"## Quando usar", "## Procedimento"} {
		if !strings.Contains(stub, want) {
			t.Fatalf("materialized runbook not scaffolded from the template (missing %q):\n%s", want, stub)
		}
	}
	ctx := mustRead(t, ctxPath)
	if !strings.Contains(ctx, "RB-0001 — Runbook A") {
		t.Fatalf("CONTEXT.md missing active runbook RB-0001:\n%s", ctx)
	}

	const frontmatter = "---\ncerne_id: RB-0001\n---\n"
	if !strings.HasPrefix(stub, frontmatter) {
		t.Fatalf("runbook stub no longer starts with the expected frontmatter:\n%s", stub)
	}
	realRB := frontmatter + "\n# Runbook A\n\nProcedimento operacional real.\n"
	if err := os.WriteFile(rbPath, []byte(realRB), 0o644); err != nil {
		t.Fatalf("write real runbook: %v", err)
	}
	mustCerne(t, "push", "--dir", proj)
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("Clone origin for verification: %v", err)
	}
	pushed := mustRead(t, filepath.Join(verify, "content", "runbooks", "RB-0001.md"))
	if pushed != realRB {
		t.Fatalf("origin runbook = %q, want the pushed content verbatim", pushed)
	}

	out = mustCerne(t, "runbook", "new", "Runbook B", "--dir", proj)
	if !strings.Contains(out, "RB-0002 created (active)") {
		t.Fatalf("second runbook new output missing RB-0002 confirmation:\n%s", out)
	}
	out = mustCerne(t, "runbook", "substituir", "RB-0001", "--by", "RB-0002", "--reason", "obsoleto", "--dir", proj)
	if !strings.Contains(out, "active") || !strings.Contains(out, "superseded") {
		t.Fatalf("substituir output missing old→new state:\n%s", out)
	}
	mustCerne(t, "pull", "--dir", proj)
	if _, err := os.Stat(rbPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("superseded runbook still materialized (stat err = %v)", err)
	}
	ctx = mustRead(t, ctxPath)
	if strings.Contains(ctx, "RB-0001") {
		t.Fatalf("CONTEXT.md still mentions superseded RB-0001:\n%s", ctx)
	}
	if !strings.Contains(ctx, "RB-0002 — Runbook B") {
		t.Fatalf("CONTEXT.md missing successor RB-0002:\n%s", ctx)
	}
}
