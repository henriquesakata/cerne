package e2e_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
)

// TestDecisionGoldenPath walks the decision (ADR) loop end to end:
// init → decision new → pull (materialized stub, listed in CONTEXT.md) →
// write the real ADR in .docs/ → push → decision new (successor) →
// decision substituir → pull (gone from .docs/), asserting the .docs/
// projection, the backend via a fresh clone of origin, and docs.lock parity.
func TestDecisionGoldenPath(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	proj := t.TempDir()
	docsDir := filepath.Join(proj, ".docs")
	ctxPath := filepath.Join(docsDir, "CONTEXT.md")
	adrPath := filepath.Join(docsDir, "current", "decisions", "D-0001-adr-a.md")

	t.Log("stage 1: init — project skeleton + backend layout pushed to origin")
	mustCerne(t, "init", "--name", "Demo", "--backend", origin, "--module", "arch", "--dir", proj)
	assertLockParity(t, proj, origin, "init")

	t.Log("stage 2: decision new — D-0001 born active")
	out := mustCerne(t, "decision", "new", "ADR A", "--dir", proj)
	if !strings.Contains(out, "D-0001 created (active)") {
		t.Fatalf("decision new output missing D-0001 confirmation:\n%s", out)
	}
	assertLockParity(t, proj, origin, "decision new")

	t.Log("stage 3: pull — ADR materialized as a stub, listed in CONTEXT.md")
	mustCerne(t, "pull", "--dir", proj)
	stub := mustRead(t, adrPath)
	for _, want := range []string{"## Contexto", "## Decisão", "## Consequências"} {
		if !strings.Contains(stub, want) {
			t.Fatalf("materialized ADR not scaffolded from the template (missing %q):\n%s", want, stub)
		}
	}
	ctx := mustRead(t, ctxPath)
	if !strings.Contains(ctx, "D-0001 — ADR A") {
		t.Fatalf("CONTEXT.md missing active decision D-0001:\n%s", ctx)
	}
	assertLockParity(t, proj, origin, "pull")

	t.Log("stage 4: write the real ADR in .docs/ and push it back to origin")
	const frontmatter = "---\ncerne_id: D-0001\n---\n"
	if !strings.HasPrefix(stub, frontmatter) {
		t.Fatalf("ADR stub no longer starts with the expected frontmatter:\n%s", stub)
	}
	realADR := frontmatter + "\n# ADR A\n\nContexto, decisão e consequências reais.\n"
	if err := os.WriteFile(adrPath, []byte(realADR), 0o644); err != nil {
		t.Fatalf("write real ADR: %v", err)
	}
	mustCerne(t, "push", "--dir", proj)
	verify := filepath.Join(t.TempDir(), "verify")
	if err := gitback.Clone(origin, verify); err != nil {
		t.Fatalf("Clone origin for verification: %v", err)
	}
	pushed := mustRead(t, filepath.Join(verify, "content", "decisions", "D-0001.md"))
	if pushed != realADR {
		t.Fatalf("origin ADR = %q, want the pushed content verbatim", pushed)
	}
	assertLockParity(t, proj, origin, "push")

	t.Log("stage 5: substituir D-0001 by D-0002 — gone from .docs/, kept in history")
	out = mustCerne(t, "decision", "new", "ADR B", "--dir", proj)
	if !strings.Contains(out, "D-0002 created (active)") {
		t.Fatalf("second decision new output missing D-0002 confirmation:\n%s", out)
	}
	out = mustCerne(t, "decision", "substituir", "D-0001", "--by", "D-0002", "--reason", "decisão revista", "--dir", proj)
	if !strings.Contains(out, "active") || !strings.Contains(out, "superseded") {
		t.Fatalf("substituir output missing old→new state:\n%s", out)
	}
	mustCerne(t, "pull", "--dir", proj)
	if _, err := os.Stat(adrPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("superseded ADR still materialized (stat err = %v)", err)
	}
	ctx = mustRead(t, ctxPath)
	if strings.Contains(ctx, "D-0001") {
		t.Fatalf("CONTEXT.md still mentions superseded D-0001:\n%s", ctx)
	}
	if !strings.Contains(ctx, "D-0002 — ADR B") {
		t.Fatalf("CONTEXT.md missing successor D-0002:\n%s", ctx)
	}
	assertLockParity(t, proj, origin, "final")
}
