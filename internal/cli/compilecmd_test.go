package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/compile"
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

func TestCompileReportsModulelessSkipped(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	// A moduled feature (synthesized) and a module-less one (omitted).
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new finance: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "new", "Sem modulo", "--dir", projDir); err != nil {
		t.Fatalf("feature new module-less: %v\n%s", err, out)
	}

	out, err := execCerne(t, "compile", "--dir", projDir)
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if !strings.Contains(out, "sem módulo não entram na wiki") {
		t.Fatalf("compile output missing module-less note:\n%s", out)
	}
}

func TestCompileNoModulelessNoteWhenNone(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	out, err := execCerne(t, "compile", "--dir", projDir)
	if err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if strings.Contains(out, "sem módulo não entram na wiki") {
		t.Fatalf("compile printed module-less note when none apply:\n%s", out)
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

// readPendingManifest decodes .docs/wiki/.pending/manifest.json for a project.
func readPendingManifest(t *testing.T, projDir string) compile.Manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(projDir, ".docs", "wiki", ".pending", compile.ManifestName))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m compile.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode manifest: %v\n%s", err, raw)
	}
	return m
}

func TestCompilePrepareWritesManifest(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance", "core")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "compile", "--dir", projDir); err != nil {
		t.Fatalf("compile prepare: %v\n%s", err, out)
	}

	m := readPendingManifest(t, projDir)
	if m.Version != compile.ManifestVersion {
		t.Fatalf("manifest version = %d, want %d", m.Version, compile.ManifestVersion)
	}
	if m.RunID == "" {
		t.Fatal("manifest run_id is empty")
	}
	var fin *compile.ManifestEntry
	for i := range m.Entries {
		if m.Entries[i].Page == "finance" {
			fin = &m.Entries[i]
		}
	}
	if fin == nil {
		t.Fatalf("no finance entry in manifest: %+v", m.Entries)
	}
	if !strings.HasSuffix(fin.PromptPath, "modules/finance.prompt.md") {
		t.Errorf("prompt_path = %q, want suffix modules/finance.prompt.md", fin.PromptPath)
	}
	// The response goes in the SAME pending tree as the prompt, not content/wiki.
	if !strings.HasSuffix(fin.ResponsePath, "modules/finance.md") || strings.Contains(fin.ResponsePath, "content/wiki") {
		t.Errorf("response_path = %q, want a .pending/ path ending modules/finance.md", fin.ResponsePath)
	}
	if fin.SourceHash == "" {
		t.Error("entry source_hash is empty")
	}
	if fin.RunID != m.RunID {
		t.Errorf("entry run_id %q != manifest run_id %q", fin.RunID, m.RunID)
	}
}
