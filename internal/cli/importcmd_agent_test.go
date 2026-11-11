package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// frigateTree is a non-hub doc layout: a state CLAUDE.md + a HISTORY.md
// changelog. The deterministic import returns zero for it; agent import does not.
func frigateTree() map[string]string {
	return map[string]string{
		"CLAUDE.md":     "# Estado atual\n\nPROJETO_FRIGATE detecta objetos.\n",
		"HISTORY.md":    "## 0.13\n- MUDANCA_GO2RTC adicionada\n",
		"docs/links.md": "Ver https://example.com/rfc\n",
	}
}

// writeAgentPlan drops a plan.json into the project's agent pending dir,
// simulating what `import --agent` (delegation) produces for the host agent.
func writeAgentPlan(t *testing.T, projDir, json string) {
	t.Helper()
	dir := filepath.Join(projDir, filepath.FromSlash(".docs/import/.pending"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plan.json"), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
}

const gabaritoPlan = `{"items":[
  {"kind":"feature","title":"Detecção go2rtc","state":"implemented","body":"# go2rtc\n\nDo changelog.","source":"HISTORY.md#go2rtc"},
  {"kind":"note","title":"Estado atual","state":"active","body":"# Estado\n\nDo CLAUDE.md.","source":"CLAUDE.md","informs":["F-0001"]},
  {"kind":"reference","title":"RFC 6749","state":"current","url":"https://example.com/rfc","body":"OAuth 2.0","source":"docs/links.md","informs":["F-0001"]}
]}`

func TestImportAgentEmitsPrompt(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	treeDir := t.TempDir()
	writeTree(t, treeDir, frigateTree())

	out, err := execCerne(t, "import", "--agent", "--root", treeDir, "--dir", projDir)
	if err != nil {
		t.Fatalf("import --agent: %v\n%s", err, out)
	}
	if !strings.Contains(out, ".docs/import/.pending/plan.prompt.md") {
		t.Fatalf("output missing prompt path:\n%s", out)
	}

	prompt, err := os.ReadFile(filepath.Join(projDir, filepath.FromSlash(".docs/import/.pending/plan.prompt.md")))
	if err != nil {
		t.Fatalf("prompt not written: %v", err)
	}
	// (crit. 1) the non-hub sources reached the prompt.
	for _, want := range []string{"PROJETO_FRIGATE", "MUDANCA_GO2RTC", "CLAUDE.md", "HISTORY.md"} {
		if !strings.Contains(string(prompt), want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	// nothing persisted on the prompt phase.
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 0 {
		t.Fatalf("prompt phase persisted %d feature(s), want 0", len(fs))
	}
}

func TestImportAgentIngestDryRunThenApply(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	writeAgentPlan(t, projDir, gabaritoPlan)

	// (crit. 5) dry-run prints the plan and writes nothing.
	out, err := execCerne(t, "import", "--agent", "--ingest", "--dir", projDir)
	if err != nil {
		t.Fatalf("ingest dry-run: %v\n%s", err, out)
	}
	for _, want := range []string{"Import plan (agent, dry-run):", "Features: 1", "Notes: 1", "References: 1", "Run with --yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 0 {
		t.Fatalf("dry-run persisted %d feature(s), want 0", len(fs))
	}

	// apply creates the entities across kinds.
	out, err = execCerne(t, "import", "--agent", "--ingest", "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("ingest --yes: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Imported (agent): 1 features, 0 decisions, 1 references, 0 runbooks, 1 notes") {
		t.Fatalf("apply confirmation missing:\n%s", out)
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 1 || fs[0].Title != "Detecção go2rtc" {
		t.Fatalf("features = %+v, want 1 (Detecção go2rtc)", fs)
	}
	if r, err := store.LoadReference(repoDir, "R-0001"); err != nil || r.URL != "https://example.com/rfc" || !r.External {
		t.Fatalf("reference = %+v err=%v", r, err)
	}
	if n, err := store.LoadNote(repoDir, "N-0001"); err != nil || len(n.Informs) != 1 {
		t.Fatalf("note = %+v err=%v", n, err)
	}

	// (crit. 5) re-running the same plan is idempotent: every source already
	// imported, nothing new. (Apply cleared the pending dir, so re-supply it —
	// as a fresh `import --agent` round would.)
	writeAgentPlan(t, projDir, gabaritoPlan)
	out, err = execCerne(t, "import", "--agent", "--ingest", "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("idempotent re-run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Everything already imported") {
		t.Fatalf("re-run not idempotent:\n%s", out)
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 1 {
		t.Fatalf("re-run duplicated features: %d, want 1", len(fs))
	}
}

func TestImportAgentIngestRejectsInvalid(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	// invalid: feature with a state that is not in the feature FSM.
	writeAgentPlan(t, projDir, `{"items":[{"kind":"feature","title":"X","state":"frobnicated","body":"b","source":"s.md"}]}`)

	_, err := execCerne(t, "import", "--agent", "--ingest", "--yes", "--dir", projDir)
	if err == nil {
		t.Fatal("ingest of invalid plan succeeded, want ErrInvalidPlan")
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 0 {
		t.Fatalf("invalid plan persisted %d feature(s), want 0", len(fs))
	}
}

func TestImportIngestWithoutAgentErrors(t *testing.T) {
	projDir, _, _ := setupProject(t)
	if _, err := execCerne(t, "import", "--ingest", "--dir", projDir); err == nil {
		t.Fatal("--ingest without --agent succeeded, want error")
	}
}
