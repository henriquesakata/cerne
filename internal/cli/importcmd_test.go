package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/importer"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// writeTree creates files under dir from rel slash path -> content.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
	}
}

// importFixtureFiles is a minimal hub-style doc tree: 2 features (one
// implemented via result.md, one candidate), 1 active ADR, 1 runbook.
func importFixtureFiles() map[string]string {
	return map[string]string{
		"docs/process/cases/0001_login/spec.md":   "# Login\n\nAutenticação via Keycloak.\n",
		"docs/process/cases/0001_login/result.md": "# Resultado\n\nEntregue.\n",
		"docs/process/cases/0002_export/spec.md":  "# Exportar CSV\n\nExportação de transações.\n",
		"docs/architecture/adr/adr-001-db.md":     "# ADR-001 — Banco por serviço\n\nDecidido: um banco por serviço.\n",
		"docs/runbooks/setup.md":                  "# Setup\n\nPassos de instalação.\n",
	}
}

func TestImportDryRun(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	writeTree(t, projDir, importFixtureFiles())
	headBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}
	lockBefore, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock before: %v", err)
	}

	out, err := execCerne(t, "import", "--dir", projDir)
	if err != nil {
		t.Fatalf("import dry-run: %v\noutput: %s", err, out)
	}
	for _, want := range []string{
		"Plano de importação (dry-run):",
		"Features: 2 (candidate 1, implemented 1)",
		"Decisões: 1 (active 1)",
		"Referências: 1",
		"Amostra:",
		"F? docs/process/cases/0001_login/ → feature implemented",
		"F? docs/process/cases/0002_export/ → feature candidate",
		"D? docs/architecture/adr/adr-001-db.md → decision active",
		"R? docs/runbooks/setup.md → reference current",
		"Rode com --yes para aplicar.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}

	// Dry-run must not touch the backend or the lock.
	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter != headBefore {
		t.Fatalf("origin HEAD changed during dry-run: %s -> %s", headBefore, headAfter)
	}
	lockAfter, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock after: %v", err)
	}
	if lockAfter.Snapshot != lockBefore.Snapshot {
		t.Fatalf("lock.Snapshot changed during dry-run: %s -> %s", lockBefore.Snapshot, lockAfter.Snapshot)
	}
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		t.Fatalf("LoadAllFeatures: %v", err)
	}
	if len(features) != 0 {
		t.Fatalf("dry-run persisted %d feature(s), want 0", len(features))
	}
}

func TestImportYes(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	treeDir := t.TempDir()
	writeTree(t, treeDir, importFixtureFiles())

	out, err := execCerne(t, "import", "--root", treeDir, "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("import --yes: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Importado: 2 features, 1 decisões, 1 referências (snapshot ") {
		t.Fatalf("output missing import confirmation:\n%s", out)
	}

	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		t.Fatalf("LoadAllFeatures: %v", err)
	}
	if len(features) != 2 {
		t.Fatalf("features = %d, want 2", len(features))
	}
	if features[0].ID != "F-0001" || features[0].State != domain.StateImplemented || features[0].Title != "Login" {
		t.Fatalf("features[0] = %+v, want F-0001 implemented Login", features[0])
	}
	if features[1].ID != "F-0002" || features[1].State != domain.StateCandidate || features[1].Title != "Exportar CSV" {
		t.Fatalf("features[1] = %+v, want F-0002 candidate Exportar CSV", features[1])
	}
	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		t.Fatalf("LoadAllDecisions: %v", err)
	}
	if len(decisions) != 1 || decisions[0].ID != "D-0001" || decisions[0].State != domain.DecisionActive {
		t.Fatalf("decisions = %+v, want one active D-0001", decisions)
	}
	references, err := store.LoadAllReferences(repoDir)
	if err != nil {
		t.Fatalf("LoadAllReferences: %v", err)
	}
	if len(references) != 1 || references[0].ID != "R-0001" || references[0].State != domain.ReferenceCurrent {
		t.Fatalf("references = %+v, want one current R-0001", references)
	}

	spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("imported spec: %v", err)
	}
	for _, want := range []string{"cerne_id: F-0001", "imported_from: docs/process/cases/0001_login/"} {
		if !strings.Contains(string(spec), want) {
			t.Fatalf("imported spec missing %q:\n%s", want, spec)
		}
	}

	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4", len(events))
	}

	// The import was committed, pushed and the lock snapshot updated.
	originHead, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if lock.Snapshot != originHead {
		t.Fatalf("lock.Snapshot = %s, want pushed origin HEAD %s", lock.Snapshot, originHead)
	}

	// Re-running WITHOUT --yes shows the plan again: no state corruption.
	out2, err := execCerne(t, "import", "--root", treeDir, "--dir", projDir)
	if err != nil {
		t.Fatalf("re-run dry-run: %v\noutput: %s", err, out2)
	}
	for _, want := range []string{"Plano de importação (dry-run):", "Rode com --yes para aplicar."} {
		if !strings.Contains(out2, want) {
			t.Errorf("re-run output missing %q:\n%s", want, out2)
		}
	}
	headAfterRerun, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after re-run: %v", err)
	}
	if headAfterRerun != originHead {
		t.Fatalf("origin HEAD changed during dry-run re-run: %s -> %s", originHead, headAfterRerun)
	}
}

func TestImportNothingToImport(t *testing.T) {
	projDir, _, _ := setupProject(t)
	out, err := execCerne(t, "import", "--dir", projDir)
	if err != nil {
		t.Fatalf("import on empty tree: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Nada para importar.") {
		t.Fatalf("output missing 'Nada para importar.':\n%s", out)
	}
}

func TestImportRootMissing(t *testing.T) {
	projDir, _, _ := setupProject(t)
	_, err := execCerne(t, "import", "--root", filepath.Join(projDir, "nope"), "--dir", projDir)
	if err == nil {
		t.Fatalf("import with missing --root succeeded, want error")
	}
}

func TestSampleLines(t *testing.T) {
	cand := func(i int) importer.Candidate {
		return importer.Candidate{Kind: "feature", State: "candidate", Source: fmt.Sprintf("docs/process/cases/%04d_x/", i)}
	}

	var small []importer.Candidate
	for i := 1; i <= 6; i++ {
		small = append(small, cand(i))
	}
	lines := sampleLines(small)
	if len(lines) != 6 {
		t.Fatalf("sampleLines(6) = %d lines, want all 6:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if lines[0] != "  F? docs/process/cases/0001_x/ → feature candidate" {
		t.Fatalf("lines[0] = %q", lines[0])
	}

	var big []importer.Candidate
	for i := 1; i <= 9; i++ {
		big = append(big, cand(i))
	}
	lines = sampleLines(big)
	if len(lines) != 7 {
		t.Fatalf("sampleLines(9) = %d lines, want first 5 + ellipsis + last:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if lines[4] != "  F? docs/process/cases/0005_x/ → feature candidate" {
		t.Fatalf("lines[4] = %q", lines[4])
	}
	if strings.TrimSpace(lines[5]) != "..." {
		t.Fatalf("lines[5] = %q, want ellipsis", lines[5])
	}
	if lines[6] != "  F? docs/process/cases/0009_x/ → feature candidate" {
		t.Fatalf("lines[6] = %q", lines[6])
	}
}
