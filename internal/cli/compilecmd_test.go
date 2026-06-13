package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/wiki"
)

func TestCompileDelegationFlow(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance", "core")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}

	// Phase 1 — prepare: only the finance page (has a live feature) gets a prompt.
	out, err := execCerne(t, "compile", "--dir", projDir)
	if err != nil {
		t.Fatalf("compile prepare: %v\n%s", err, out)
	}
	pending := filepath.Join(projDir, ".docs", "wiki", ".pending", "modules", "finance.prompt.md")
	if _, err := os.Stat(pending); err != nil {
		t.Fatalf("expected pending prompt at %s: %v\noutput: %s", pending, err, out)
	}
	if !strings.Contains(out, "--ingest") || !strings.Contains(out, "finance") {
		t.Fatalf("prepare output missing instruction/page:\n%s", out)
	}
	// Empty pages (core, project) must not be prepared.
	if _, err := os.Stat(filepath.Join(projDir, ".docs", "wiki", ".pending", "modules", "core.prompt.md")); err == nil {
		t.Fatalf("core page (no sources) should not be prepared")
	}

	// The host agent synthesizes the page into the backend.
	pagePath := filepath.Join(repoDir, "content", "wiki", "modules", "finance.md")
	if err := os.MkdirAll(filepath.Dir(pagePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pagePath, []byte("# Finance\n\nGera boletos.\n"), 0o644); err != nil {
		t.Fatalf("agent write page: %v", err)
	}

	// Phase 2 — ingest: stamp provenance, commit, clear pending.
	out, err = execCerne(t, "compile", "--ingest", "--dir", projDir)
	if err != nil {
		t.Fatalf("compile --ingest: %v\n%s", err, out)
	}
	stamped, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatalf("read stamped page: %v", err)
	}
	prov := wiki.ParseProvenance(stamped)
	if prov.Page != "finance" || prov.InputHash == "" {
		t.Fatalf("page not stamped: %+v", prov)
	}
	if len(prov.Sources) != 1 || prov.Sources[0] != "F-0001" {
		t.Fatalf("provenance sources = %v, want [F-0001]", prov.Sources)
	}
	if !strings.Contains(string(stamped), "Gera boletos") {
		t.Fatalf("ingest dropped the agent's body:\n%s", stamped)
	}
	if _, err := os.Stat(pending); err == nil {
		t.Fatalf("pending prompt not cleared after ingest")
	}

	// Re-running compile finds nothing dirty.
	out, err = execCerne(t, "compile", "--dir", projDir)
	if err != nil {
		t.Fatalf("compile re-run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Nada a recompilar") {
		t.Fatalf("expected nothing-dirty message:\n%s", out)
	}
}

func TestCompileForceRecompiles(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	// Compile once.
	if _, err := execCerne(t, "compile", "--dir", projDir); err != nil {
		t.Fatalf("compile: %v", err)
	}
	page := filepath.Join(repoDir, "content", "wiki", "modules", "finance.md")
	_ = os.MkdirAll(filepath.Dir(page), 0o755)
	_ = os.WriteFile(page, []byte("# Finance\n\nv1.\n"), 0o644)
	if _, err := execCerne(t, "compile", "--ingest", "--dir", projDir); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	// Clean now; --force makes it dirty again (prompt re-emitted).
	out, err := execCerne(t, "compile", "--force", "--dir", projDir)
	if err != nil {
		t.Fatalf("compile --force: %v\n%s", err, out)
	}
	if strings.Contains(out, "Nada a recompilar") {
		t.Fatalf("--force should recompile, got:\n%s", out)
	}
}
