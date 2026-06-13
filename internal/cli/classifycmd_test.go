package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// classifySeed describes one feature to plant in the backend: its module (empty
// = module-less), the imported_from path the importer would have injected, and
// an optional tags line in the spec body.
type classifySeed struct {
	id           string
	module       string
	importedFrom string
	tagsLine     string
}

// classifyFixture mirrors what the importer produces, but is planted DIRECTLY
// (not via `cerne import`) so the fixture is independent of the importer's own
// inference — keeping these tests valid after the importer learns to classify:
//   - F-0001: folder token "finance" → assignable;
//   - F-0002: tag "#identity" → assignable;
//   - F-0003: no module signal → skipped (cross-cutting);
//   - F-0004: two module tokens → skipped (ambiguous);
//   - F-0005: already moduled "finance" → untouched.
func classifyFixture() []classifySeed {
	return []classifySeed{
		{"F-0001", "", "docs/process/cases/0021_finance-consolidation/", ""},
		{"F-0002", "", "docs/process/cases/0033_data-migration/", "tags: #spec #identity"},
		{"F-0003", "", "docs/process/cases/0050_platform-vision/", "tags: #spec #planning"},
		{"F-0004", "", "docs/process/cases/0099_finance-identity-bridge/", ""},
		{"F-0005", "finance", "apps/finance/docs/process/cases/0001_extrato/", ""},
	}
}

// seedClassifyBackend inits a project (modules finance/identity), plants the
// fixture features into the backend clone and pushes them to origin so the
// pull-first classify command sees them. Returns the resolved dirs.
func seedClassifyBackend(t *testing.T) (projDir, origin, repoDir string) {
	t.Helper()
	projDir, origin, repoDir = setupProject(t, "finance", "identity")
	now := time.Now().UTC()
	for _, s := range classifyFixture() {
		f := store.Feature{ID: s.id, Title: s.id, Module: s.module, State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", s.id, err)
		}
		body := "---\ncerne_id: " + s.id + "\nimported_from: " + s.importedFrom + "\n---\n\n# " + s.id + "\n\n"
		if s.tagsLine != "" {
			body += s.tagsLine + "\n\n"
		}
		body += "Corpo.\n"
		writeTree(t, repoDir, map[string]string{filepath.ToSlash(filepath.Join("content", "features", s.id, "spec.md")): body})
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed: classify fixture"); err != nil {
		t.Fatalf("seed CommitAndPush: %v", err)
	}
	return projDir, origin, repoDir
}

// moduleByID indexes the backend's features by ID -> module.
func moduleByID(t *testing.T, repoDir string) map[string]string {
	t.Helper()
	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		t.Fatalf("LoadAllFeatures: %v", err)
	}
	m := map[string]string{}
	for _, f := range features {
		m[f.ID] = f.Module
	}
	return m
}

func TestClassifyDryRun(t *testing.T) {
	projDir, origin, repoDir := seedClassifyBackend(t)
	headBefore, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA before: %v", err)
	}
	modsBefore := moduleByID(t, repoDir)

	out, err := execCerne(t, "classify", "--dir", projDir)
	if err != nil {
		t.Fatalf("classify dry-run: %v\noutput: %s", err, out)
	}
	// Lists the two inferable features and nothing else.
	for _, want := range []string{
		"F-0001 → finance",  // 0021_finance-consolidation
		"F-0002 → identity", // 0033_identity-roles
		"2 feature(s) sem módulo permanecem (transversais/ambíguas).",
		"Rode com --yes para aplicar.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
	// The ambiguous/cross-cutting features must NOT appear as assignments.
	if strings.Contains(out, "F-0003 →") || strings.Contains(out, "F-0004 →") {
		t.Errorf("dry-run proposed an assignment for an ambiguous/cross-cutting feature:\n%s", out)
	}

	// Dry-run touches nothing: origin HEAD and every module unchanged.
	headAfter, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after: %v", err)
	}
	if headAfter != headBefore {
		t.Fatalf("origin HEAD changed during dry-run: %s -> %s", headBefore, headAfter)
	}
	for id, mod := range modsBefore {
		if moduleByID(t, repoDir)[id] != mod {
			t.Fatalf("%s module changed during dry-run: %q -> %q", id, mod, moduleByID(t, repoDir)[id])
		}
	}
}

func TestClassifyYes(t *testing.T) {
	projDir, origin, repoDir := seedClassifyBackend(t)
	eventsBefore, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents before: %v", err)
	}

	out, err := execCerne(t, "classify", "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("classify --yes: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Classificadas 2 feature(s).") {
		t.Fatalf("output missing apply confirmation:\n%s", out)
	}

	// Only the two inferable features got a module; ambiguous/cross-cutting and
	// the already-moduled apps/finance feature are exactly as before.
	mods := moduleByID(t, repoDir)
	wantMods := map[string]string{
		"F-0001": "finance",  // assigned
		"F-0002": "identity", // assigned
		"F-0003": "",         // 0050_platform-vision, cross-cutting
		"F-0004": "",         // 0099_finance-identity-bridge, ambiguous
		"F-0005": "finance",  // apps/finance, born moduled
	}
	for id, want := range wantMods {
		if mods[id] != want {
			t.Errorf("%s.Module = %q, want %q", id, mods[id], want)
		}
	}

	// One feature.classified event (source heuristic) per assignment.
	eventsAfter, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents after: %v", err)
	}
	classified := map[string]string{}
	for _, ev := range eventsAfter {
		if ev.Type == "feature.classified" {
			if ev.Payload["source"] != "heuristic" {
				t.Errorf("event %s source = %q, want heuristic", ev.EntityID, ev.Payload["source"])
			}
			classified[ev.EntityID] = ev.Payload["module"]
		}
	}
	if len(classified) != 2 || classified["F-0001"] != "finance" || classified["F-0002"] != "identity" {
		t.Fatalf("feature.classified events = %v, want F-0001->finance, F-0002->identity", classified)
	}
	if len(eventsAfter) != len(eventsBefore)+2 {
		t.Fatalf("events grew by %d, want exactly 2", len(eventsAfter)-len(eventsBefore))
	}

	// Committed, pushed, lock snapshot == origin HEAD.
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

	// Idempotent: the now-moduled features are skipped, nothing left to assign.
	out2, err := execCerne(t, "classify", "--yes", "--dir", projDir)
	if err != nil {
		t.Fatalf("second classify --yes: %v\noutput: %s", err, out2)
	}
	if !strings.Contains(out2, "Nada a classificar.") {
		t.Fatalf("second --yes output missing 'Nada a classificar.':\n%s", out2)
	}
	headAfter2, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA after second run: %v", err)
	}
	if headAfter2 != originHead {
		t.Fatalf("origin HEAD changed on idempotent re-run: %s -> %s", originHead, headAfter2)
	}
}

func TestClassifyManualAssign(t *testing.T) {
	projDir, origin, repoDir := seedClassifyBackend(t)

	// F-0003 is module-less and cross-cutting (the heuristic skips it); manual
	// assignment overrides that with an explicit, precise module.
	out, err := execCerne(t, "classify", "F-0003", "--module", "identity", "--dir", projDir)
	if err != nil {
		t.Fatalf("manual classify: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0003 → identity.") {
		t.Fatalf("output missing manual confirmation:\n%s", out)
	}

	f, err := store.LoadFeature(repoDir, "F-0003")
	if err != nil {
		t.Fatalf("LoadFeature F-0003: %v", err)
	}
	if f.Module != "identity" {
		t.Fatalf("F-0003.Module = %q, want identity", f.Module)
	}

	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := events[len(events)-1]
	if last.Type != "feature.classified" || last.EntityID != "F-0003" ||
		last.Payload["module"] != "identity" || last.Payload["source"] != "manual" {
		t.Fatalf("last event = %+v, want feature.classified F-0003 module identity source manual", last)
	}

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
}

func TestClassifyManualUnknownModule(t *testing.T) {
	projDir, _, repoDir := seedClassifyBackend(t)
	_, err := execCerne(t, "classify", "F-0003", "--module", "marketing", "--dir", projDir)
	if err == nil {
		t.Fatalf("manual classify with undeclared module succeeded, want error")
	}
	if !strings.Contains(err.Error(), "marketing") || !strings.Contains(err.Error(), "finance") {
		t.Fatalf("error %q should name the bad module and the declared ones", err)
	}
	f, err := store.LoadFeature(repoDir, "F-0003")
	if err != nil {
		t.Fatalf("LoadFeature F-0003: %v", err)
	}
	if f.Module != "" {
		t.Fatalf("F-0003.Module = %q after failed manual assign, want unchanged empty", f.Module)
	}
}

func TestClassifyManualUnknownID(t *testing.T) {
	projDir, _, _ := seedClassifyBackend(t)
	_, err := execCerne(t, "classify", "F-9999", "--module", "finance", "--dir", projDir)
	if err == nil {
		t.Fatalf("manual classify of unknown id succeeded, want error")
	}
	if !strings.Contains(err.Error(), "F-9999") {
		t.Fatalf("error %q should name the missing id", err)
	}
}

func TestClassifyManualReclassifiesModuled(t *testing.T) {
	// Manual mode is precise and allowed to reclassify an already-moduled
	// feature (unlike the backfill, which never touches moduled features).
	projDir, _, repoDir := seedClassifyBackend(t)
	if _, err := execCerne(t, "classify", "F-0005", "--module", "identity", "--dir", projDir); err != nil {
		t.Fatalf("manual reclassify: %v", err)
	}
	f, err := store.LoadFeature(repoDir, "F-0005")
	if err != nil {
		t.Fatalf("LoadFeature F-0005: %v", err)
	}
	if f.Module != "identity" {
		t.Fatalf("F-0005.Module = %q, want identity (reclassified from finance)", f.Module)
	}
}

func TestClassifyManualRequiresModuleFlag(t *testing.T) {
	projDir, _, _ := seedClassifyBackend(t)
	_, err := execCerne(t, "classify", "F-0003", "--dir", projDir)
	if err == nil {
		t.Fatalf("manual classify without --module succeeded, want error")
	}
}
