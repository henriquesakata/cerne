package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/importer"
	"github.com/henriquesakata/cerne/internal/store"
)

// anchorPlan has one anchored feature, one decision whose state is invented
// (its evidence is not in the source) and one note without evidence.
const anchorPlan = `{"items":[
  {"kind":"feature","title":"Detecção go2rtc","state":"implemented","body":"b1","source":"HISTORY.md#go2rtc","evidence":["MUDANCA_GO2RTC adicionada"]},
  {"kind":"decision","title":"Adotou Kubernetes","state":"active","body":"b2","source":"HISTORY.md#k8s","evidence":["migrou para Kubernetes"]},
  {"kind":"note","title":"Sem prova","state":"active","body":"b3","source":"CLAUDE.md"}
]}`

// fixedPlan is anchorPlan with the evidence corrected, as a human would.
const fixedPlan = `{"items":[
  {"kind":"feature","title":"Detecção go2rtc","state":"implemented","body":"b1","source":"HISTORY.md#go2rtc","evidence":["MUDANCA_GO2RTC adicionada"]},
  {"kind":"decision","title":"Adotou Kubernetes","state":"active","body":"b2","source":"HISTORY.md#k8s","evidence":["MUDANCA_GO2RTC adicionada"]},
  {"kind":"note","title":"Sem prova","state":"active","body":"b3","source":"CLAUDE.md","evidence":["PROJETO_FRIGATE detecta objetos."]}
]}`

func pendingPlanExists(projDir string) bool {
	_, err := os.Stat(filepath.Join(projDir, filepath.FromSlash(".docs/import/.pending/plan.json")))
	return err == nil
}

// F-0500 crit. 6: the dry-run lists anchored and unanchored items in separate
// blocks, each unanchored one with its reason.
func TestImportAgentDryRunListsUnanchored(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	writeTree(t, projDir, frigateTree())
	writeAgentPlan(t, projDir, anchorPlan)

	out, err := execCerne(t, "import", "--agent", "--ingest", "--dir", projDir)
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Anchored (will be imported): 1",
		"HISTORY.md:2-2",
		"Unanchored (left out): 2",
		`decision active "Adotou Kubernetes" ← HISTORY.md#k8s: evidence not found in HISTORY.md: "migrou para Kubernetes"`,
		`note active "Sem prova" ← CLAUDE.md: no evidence`,
		"Run with --yes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run missing %q:\n%s", want, out)
		}
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 0 {
		t.Fatalf("dry-run persisted %d feature(s)", len(fs))
	}
}

// F-0500 crit. 7 and 9: --yes applies only the anchored items, reports the
// rest and keeps the plan; fixing the evidence and re-ingesting applies the
// remainder without duplicating what is already in.
func TestImportAgentYesAppliesOnlyAnchoredThenReingest(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	writeTree(t, projDir, frigateTree())
	writeAgentPlan(t, projDir, anchorPlan)

	out, err := execCerne(t, "import", "--agent", "--ingest", "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("--yes: %v\n%s", err, out)
	}
	for _, want := range []string{"Imported (agent): 1 features, 0 decisions", "Left out 2 item(s) without an anchor", "no evidence", "evidence not found"} {
		if !strings.Contains(out, want) {
			t.Errorf("--yes output missing %q:\n%s", want, out)
		}
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 1 {
		t.Fatalf("features = %d, want 1", len(fs))
	}
	if ds, _ := store.LoadAllDecisions(repoDir); len(ds) != 0 {
		t.Fatalf("an unanchored decision was applied: %+v", ds)
	}
	if !pendingPlanExists(projDir) {
		t.Fatal("plan.json was removed although items were left out")
	}

	// the human fixes the evidence in plan.json and ingests again.
	writeAgentPlan(t, projDir, fixedPlan)
	out, err = execCerne(t, "import", "--agent", "--ingest", "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("re-ingest: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Imported (agent): 0 features, 1 decisions, 0 references, 0 runbooks, 1 notes") {
		t.Fatalf("re-ingest should apply only the remainder:\n%s", out)
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 1 {
		t.Fatalf("re-ingest duplicated the feature: %d", len(fs))
	}
	if ds, _ := store.LoadAllDecisions(repoDir); len(ds) != 1 {
		t.Fatalf("decisions = %d, want 1", len(ds))
	}
	if pendingPlanExists(projDir) {
		t.Fatal("plan.json kept although nothing was left out")
	}
}

// F-0500 crit. 7: with nothing anchored, --yes writes nothing and keeps the plan.
func TestImportAgentYesNothingAnchored(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	writeAgentPlan(t, projDir, `{"items":[{"kind":"note","title":"X","state":"active","body":"b","source":"GONE.md","evidence":["x"]}]}`)

	out, err := execCerne(t, "import", "--agent", "--ingest", "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("--yes: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Left out 1 item(s) without an anchor") || !strings.Contains(out, "source not found: GONE.md") {
		t.Fatalf("output:\n%s", out)
	}
	if ns, _ := store.LoadAllNotes(repoDir); len(ns) != 0 {
		t.Fatalf("notes = %d, want 0", len(ns))
	}
	if !pendingPlanExists(projDir) {
		t.Fatal("plan.json was removed")
	}
}

// F-0500 crit. 9: a schema error still drops the whole plan, even next to
// well-anchored items.
func TestImportAgentSchemaErrorStillDropsPlan(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	writeTree(t, projDir, frigateTree())
	writeAgentPlan(t, projDir, `{"items":[
  {"kind":"feature","title":"Bom","state":"implemented","body":"b","source":"HISTORY.md","evidence":["MUDANCA_GO2RTC adicionada"]},
  {"kind":"feature","title":"Ruim","state":"frobnicated","body":"b","source":"HISTORY.md","evidence":["MUDANCA_GO2RTC adicionada"]}
]}`)

	_, err := execCerne(t, "import", "--agent", "--ingest", "--yes", "--dir", projDir)
	if !errors.Is(err, importer.ErrInvalidPlan) {
		t.Fatalf("err = %v, want ErrInvalidPlan", err)
	}
	if fs, _ := store.LoadAllFeatures(repoDir); len(fs) != 0 {
		t.Fatalf("persisted %d feature(s), want 0", len(fs))
	}
}
