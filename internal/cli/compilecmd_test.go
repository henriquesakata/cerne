package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/compile"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/wiki"
)

func TestCompileDelegationFlow(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance", "core")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
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

	// The host agent writes its synthesis to the declared response_path in the
	// pending tree, echoing the run id so ingest accepts it as fresh.
	m := readPendingManifest(t, projDir)
	if len(m.Entries) != 1 || m.Entries[0].Page != "finance" {
		t.Fatalf("manifest entries = %+v, want one finance entry", m.Entries)
	}
	respPath := filepath.Join(projDir, filepath.FromSlash(m.Entries[0].ResponsePath))
	agentBody := "---\nrun_id: " + m.Entries[0].RunID + "\n---\n# Finance\n\nGera boletos.\n"
	if err := os.WriteFile(respPath, []byte(agentBody), 0o644); err != nil {
		t.Fatalf("agent write response: %v", err)
	}

	// The stamped page lands in the backend clone here.
	pagePath := filepath.Join(repoDir, "content", "wiki", "modules", "finance.md")

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
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new finance: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "new", "Sem modulo", "--context", "ctx de teste", "--dir", projDir); err != nil {
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
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
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
	projDir, _, _ := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	// Compile once.
	if _, err := execCerne(t, "compile", "--dir", projDir); err != nil {
		t.Fatalf("compile: %v", err)
	}
	fulfillPending(t, projDir, "# Finance\n\nv1.\n")
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

// fulfillPending simulates the host agent: for each manifest entry it writes a
// fresh response (echoing the run id, so ingest accepts it) at the declared
// response_path.
func fulfillPending(t *testing.T, projDir, body string) {
	t.Helper()
	m := readPendingManifest(t, projDir)
	for _, e := range m.Entries {
		p := filepath.Join(projDir, filepath.FromSlash(e.ResponsePath))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir response dir: %v", err)
		}
		content := "---\nrun_id: " + e.RunID + "\n---\n" + body
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write response %s: %v", e.ResponsePath, err)
		}
	}
}

func TestCompilePrepareWritesManifest(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance", "core")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
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

func TestCompileIngestMissingResponseErrors(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance", "core")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "compile", "--dir", projDir); err != nil {
		t.Fatalf("compile prepare: %v\n%s", err, out)
	}
	// Agent writes nothing — ingest must fail and name the exact expected path.
	out, err := execCerne(t, "compile", "--ingest", "--dir", projDir)
	if err == nil {
		t.Fatalf("expected ingest to fail on missing response, got success:\n%s", out)
	}
	if !strings.Contains(out, "modules/finance.md") {
		t.Fatalf("error should name the expected response path, got:\n%s", out)
	}
}

func TestCompileIngestRejectsStale(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance", "core")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "compile", "--dir", projDir); err != nil {
		t.Fatalf("compile prepare: %v\n%s", err, out)
	}
	// Agent writes a STALE response: a run_id from a different round (repro of
	// the bug where an old page in the clone passed as "Compilado").
	m := readPendingManifest(t, projDir)
	respPath := filepath.Join(projDir, filepath.FromSlash(m.Entries[0].ResponsePath))
	if err := os.MkdirAll(filepath.Dir(respPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(respPath, []byte("---\nrun_id: rodada-anterior\n---\n# velho\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stale alone is not a hard error — it's reported and skipped.
	out, err := execCerne(t, "compile", "--ingest", "--dir", projDir)
	if err != nil {
		t.Fatalf("stale-only ingest should not hard-error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "obsoleto") {
		t.Fatalf("expected the page reported obsoleto, got:\n%s", out)
	}
	if !strings.Contains(out, "Compilado: 0") {
		t.Fatalf("stale page must not count as compiled, got:\n%s", out)
	}
	// The stale body must NOT have been stamped into the backend clone.
	stamped := filepath.Join(repoDir, "content", "wiki", "modules", "finance.md")
	if data, err := os.ReadFile(stamped); err == nil && strings.Contains(string(data), "velho") {
		t.Fatalf("stale body leaked into the backend page:\n%s", data)
	}
}

func TestCompileIngestPerPageStatus(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance", "billing")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new finance: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "new", "Faturas", "--context", "ctx de teste", "--module", "billing", "--dir", projDir); err != nil {
		t.Fatalf("feature new billing: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "compile", "--dir", projDir); err != nil {
		t.Fatalf("prepare: %v\n%s", err, out)
	}
	// finance: fresh (echoes run id) → ingerido; billing: stale → obsoleto.
	m := readPendingManifest(t, projDir)
	for _, e := range m.Entries {
		respPath := filepath.Join(projDir, filepath.FromSlash(e.ResponsePath))
		if err := os.MkdirAll(filepath.Dir(respPath), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nrun_id: rodada-velha\n---\n# " + e.Page + " velho\n"
		if e.Page == "finance" {
			body = "---\nrun_id: " + e.RunID + "\n---\n# Finance\n"
		}
		if err := os.WriteFile(respPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, err := execCerne(t, "compile", "--ingest", "--dir", projDir)
	if err != nil {
		t.Fatalf("ingest: %v\n%s", err, out)
	}
	if !strings.Contains(out, "finance: ingerido") {
		t.Errorf("expected finance ingerido, got:\n%s", out)
	}
	if !strings.Contains(out, "billing: obsoleto") {
		t.Errorf("expected billing obsoleto, got:\n%s", out)
	}
	if !strings.Contains(out, "Compilado: 1") {
		t.Errorf("expected Compilado: 1 (only the fresh page), got:\n%s", out)
	}
}

func TestCompileHelpShowsDelegationContract(t *testing.T) {
	out, err := execCerne(t, "compile", "--help")
	if err != nil {
		t.Fatalf("compile --help: %v\n%s", err, out)
	}
	// The round-trip (crit. 1) + the machine contract it points at (crit. 3).
	for _, want := range []string{".docs/wiki/.pending/", "manifest.json", "response_path", "cerne compile --ingest"} {
		if !strings.Contains(out, want) {
			t.Errorf("compile --help missing %q:\n%s", want, out)
		}
	}
}

func TestCompileIngestIdempotent(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance", "core")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--context", "ctx de teste", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "compile", "--dir", projDir); err != nil {
		t.Fatalf("prepare: %v\n%s", err, out)
	}
	fulfillPending(t, projDir, "# Finance\n\nv1.\n")
	if out, err := execCerne(t, "compile", "--ingest", "--dir", projDir); err != nil {
		t.Fatalf("first ingest: %v\n%s", err, out)
	}
	headBefore, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	// Second ingest: pending was cleared, so it's a no-op with no new commit.
	out, err := execCerne(t, "compile", "--ingest", "--dir", projDir)
	if err != nil {
		t.Fatalf("second ingest: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Nenhuma página pendente") {
		t.Fatalf("expected nothing-pending message, got:\n%s", out)
	}
	headAfter, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if headBefore != headAfter {
		t.Fatalf("second ingest created a commit: %s -> %s", headBefore, headAfter)
	}
}
