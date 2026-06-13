package importer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

const hubtree = "testdata/hubtree"

func TestScanHubTree(t *testing.T) {
	cands, err := Scan(hubtree, []string{"finance", "identity"})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	want := []struct {
		kind, source, state, module, title string
		files                              []string
	}{
		{"feature", "docs/process/cases/0001_login-google/", "implemented", "", "Login com Google", []string{"plan.md", "result.md", "spec.md"}},
		{"feature", "docs/process/cases/0002_export-csv/", "candidate", "", "Export csv", []string{"spec.md"}},
		{"feature", "docs/process/cases/0003_old-billing/", "superseded", "", "Cobrança antiga", []string{"spec.md"}},
		// Root case whose folder names a single declared module → inferred at scan.
		{"feature", "docs/process/cases/0004_finance-reporting/", "candidate", "finance", "Relatórios financeiros", []string{"spec.md"}},
		// Root case whose folder names TWO declared modules → ambiguous, stays module-less.
		{"feature", "docs/process/cases/0005_finance-identity-merge/", "candidate", "", "Fusão finance e identity", []string{"spec.md"}},
		{"feature", "apps/finance/docs/process/cases/0001_extrato/", "in_development", "finance", "Extrato consolidado", []string{"plan.md", "spec.md"}},
		{"decision", "docs/architecture/adr/adr-001-database-per-service.md", "active", "", "ADR-001 — Database per service", []string{"doc.md"}},
		{"decision", "docs/architecture/adr/adr-002-monolith-auth.md", "superseded", "", "ADR-002 — Autenticação no monólito", []string{"doc.md"}},
		{"reference", "docs/architecture/overview.md", "current", "", "Overview", []string{"doc.md"}},
		{"reference", "docs/planning/features.md", "current", "", "Features", []string{"doc.md"}},
		{"reference", "docs/runbooks/dev-setup.md", "current", "", "Setup de desenvolvimento", []string{"doc.md"}},
	}
	if len(cands) != len(want) {
		t.Fatalf("Scan returned %d candidates, want %d: %+v", len(cands), len(want), cands)
	}
	for i, w := range want {
		c := cands[i]
		if c.Kind != w.kind || c.Source != w.source || c.State != w.state || c.Module != w.module || c.Title != w.title {
			t.Errorf("cands[%d] = {Kind:%q Source:%q State:%q Module:%q Title:%q}, want {%q %q %q %q %q}",
				i, c.Kind, c.Source, c.State, c.Module, c.Title, w.kind, w.source, w.state, w.module, w.title)
		}
		keys := make([]string, 0, len(c.Files))
		for k := range c.Files {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, w.files) {
			t.Errorf("cands[%d].Files keys = %v, want %v", i, keys, w.files)
		}
		for dest, src := range c.Files {
			if !filepath.IsAbs(src) {
				t.Errorf("cands[%d].Files[%q] = %q, want absolute path", i, dest, src)
			}
			if _, err := os.Stat(src); err != nil {
				t.Errorf("cands[%d].Files[%q]: %v", i, dest, err)
			}
		}
	}
}

func TestScanInfersModuleForRootCases(t *testing.T) {
	// Root cases (no path module) get a module inferred from folder/tags when a
	// single declared module matches; apps/<app> cases keep their path module.
	cands, err := Scan(hubtree, []string{"finance", "identity"})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	bySource := map[string]Candidate{}
	for _, c := range cands {
		bySource[c.Source] = c
	}
	if got := bySource["docs/process/cases/0004_finance-reporting/"].Module; got != "finance" {
		t.Errorf("0004_finance-reporting module = %q, want finance (single-match inference)", got)
	}
	if got := bySource["docs/process/cases/0005_finance-identity-merge/"].Module; got != "" {
		t.Errorf("0005_finance-identity-merge module = %q, want empty (ambiguous, two modules)", got)
	}
	if got := bySource["docs/process/cases/0001_login-google/"].Module; got != "" {
		t.Errorf("0001_login-google module = %q, want empty (no module signal)", got)
	}
	if got := bySource["apps/finance/docs/process/cases/0001_extrato/"].Module; got != "finance" {
		t.Errorf("apps/finance extrato module = %q, want finance (path module preserved)", got)
	}
}

func TestScanWithoutModulesLeavesRootCasesModuleLess(t *testing.T) {
	// With no declared modules the inference has nothing to match: root cases
	// stay module-less, exactly as before this feature.
	cands, err := Scan(hubtree, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, c := range cands {
		if c.Kind == "feature" && strings.HasPrefix(c.Source, "docs/process/cases/") && c.Module != "" {
			t.Errorf("root case %s got module %q with no declared modules, want empty", c.Source, c.Module)
		}
	}
}

func TestScanRootWithoutDocs(t *testing.T) {
	cands, err := Scan(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Scan on empty root: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("Scan on empty root = %+v, want no candidates", cands)
	}
}

func TestApplyHubTree(t *testing.T) {
	repoDir := t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "demo", Modules: []string{"finance", "identity"}}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	cands, err := Scan(hubtree, []string{"finance", "identity"})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	sum, err := Apply(repoDir, cands, "importer-test")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if sum.Features != 6 || sum.Decisions != 2 || sum.References != 3 {
		t.Fatalf("Summary counts = %+v, want 6 features, 2 decisions, 3 references", sum)
	}
	wantByState := map[string]int{
		"implemented": 1, "candidate": 3, "superseded": 2, "in_development": 1,
		"active": 1, "current": 3,
	}
	for state, n := range wantByState {
		if sum.ByState[state] != n {
			t.Errorf("ByState[%q] = %d, want %d (full: %v)", state, sum.ByState[state], n, sum.ByState)
		}
	}

	features, err := store.LoadAllFeatures(repoDir)
	if err != nil {
		t.Fatalf("LoadAllFeatures: %v", err)
	}
	if len(features) != 6 {
		t.Fatalf("features = %d, want 6", len(features))
	}
	wantFeatures := []struct {
		id     string
		state  domain.FeatureState
		module string
		title  string
	}{
		{"F-0001", domain.StateImplemented, "", "Login com Google"},
		{"F-0002", domain.StateCandidate, "", "Export csv"},
		{"F-0003", domain.StateSuperseded, "", "Cobrança antiga"},
		// 0004_finance-reporting: single-module folder token → inferred finance.
		{"F-0004", domain.StateCandidate, "finance", "Relatórios financeiros"},
		// 0005_finance-identity-merge: two module tokens → ambiguous, module-less.
		{"F-0005", domain.StateCandidate, "", "Fusão finance e identity"},
		{"F-0006", domain.StateInDevelopment, "finance", "Extrato consolidado"},
	}
	for i, w := range wantFeatures {
		f := features[i]
		if f.ID != w.id || f.State != w.state || f.Module != w.module || f.Title != w.title {
			t.Errorf("features[%d] = %+v, want id %s state %s module %q title %q", i, f, w.id, w.state, w.module, w.title)
		}
		if f.CreatedAt.IsZero() || !f.UpdatedAt.Equal(f.CreatedAt) {
			t.Errorf("features[%d] timestamps = created %v updated %v, want equal non-zero", i, f.CreatedAt, f.UpdatedAt)
		}
		if f.CreatedAt.Location() != time.UTC {
			t.Errorf("features[%d].CreatedAt location = %v, want UTC", i, f.CreatedAt.Location())
		}
	}
	if features[2].Reason != "importado como supersedido" || features[2].SupersededBy != "" {
		t.Errorf("superseded feature = %+v, want Reason 'importado como supersedido' and empty SupersededBy", features[2])
	}
	if features[0].Reason != "" {
		t.Errorf("implemented feature Reason = %q, want empty", features[0].Reason)
	}

	decisions, err := store.LoadAllDecisions(repoDir)
	if err != nil {
		t.Fatalf("LoadAllDecisions: %v", err)
	}
	if len(decisions) != 2 || decisions[0].ID != "D-0001" || decisions[1].ID != "D-0002" {
		t.Fatalf("decisions = %+v, want D-0001 and D-0002", decisions)
	}
	if decisions[0].State != domain.DecisionActive || decisions[1].State != domain.DecisionSuperseded {
		t.Fatalf("decision states = %s, %s, want active, superseded", decisions[0].State, decisions[1].State)
	}

	references, err := store.LoadAllReferences(repoDir)
	if err != nil {
		t.Fatalf("LoadAllReferences: %v", err)
	}
	if len(references) != 3 || references[0].ID != "R-0001" || references[1].ID != "R-0002" || references[2].ID != "R-0003" {
		t.Fatalf("references = %+v, want R-0001, R-0002 and R-0003", references)
	}
	if references[1].Title != "Features" {
		t.Fatalf("references[1].Title = %q, want Features (docs/planning sorted between architecture and runbooks)", references[1].Title)
	}
	for _, r := range references {
		if r.State != domain.ReferenceCurrent {
			t.Errorf("reference %s state = %s, want current", r.ID, r.State)
		}
	}

	// Frontmatter prepend branch: source file has no frontmatter block.
	spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("imported spec: %v", err)
	}
	wantSpecPrefix := "---\ncerne_id: F-0001\nimported_from: docs/process/cases/0001_login-google/\n---\n\n# Login com Google"
	if !strings.HasPrefix(string(spec), wantSpecPrefix) {
		t.Errorf("imported spec does not start with injected frontmatter:\n%s", spec)
	}
	for _, name := range []string{"plan.md", "result.md"} {
		if _, err := os.Stat(filepath.Join(repoDir, "content", "features", "F-0001", name)); err != nil {
			t.Errorf("imported %s: %v", name, err)
		}
	}

	// Frontmatter inject branch: source file already has a frontmatter block;
	// cerne_id and imported_from become the first keys, existing keys stay.
	ref, err := os.ReadFile(filepath.Join(repoDir, "content", "references", "R-0003.md"))
	if err != nil {
		t.Fatalf("imported reference: %v", err)
	}
	wantRefPrefix := "---\ncerne_id: R-0003\nimported_from: docs/runbooks/dev-setup.md\ntags: runbook\n---\n\n# Setup de desenvolvimento"
	if !strings.HasPrefix(string(ref), wantRefPrefix) {
		t.Errorf("imported reference frontmatter not injected as first keys:\n%s", ref)
	}

	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 11 {
		t.Fatalf("events = %d, want 11", len(events))
	}
	wantEvents := []struct{ entityID, evType, source string }{
		{"F-0001", "feature.imported", "docs/process/cases/0001_login-google/"},
		{"F-0002", "feature.imported", "docs/process/cases/0002_export-csv/"},
		{"F-0003", "feature.imported", "docs/process/cases/0003_old-billing/"},
		{"F-0004", "feature.imported", "docs/process/cases/0004_finance-reporting/"},
		{"F-0005", "feature.imported", "docs/process/cases/0005_finance-identity-merge/"},
		{"F-0006", "feature.imported", "apps/finance/docs/process/cases/0001_extrato/"},
		{"D-0001", "decision.imported", "docs/architecture/adr/adr-001-database-per-service.md"},
		{"D-0002", "decision.imported", "docs/architecture/adr/adr-002-monolith-auth.md"},
		{"R-0001", "reference.imported", "docs/architecture/overview.md"},
		{"R-0002", "reference.imported", "docs/planning/features.md"},
		{"R-0003", "reference.imported", "docs/runbooks/dev-setup.md"},
	}
	for i, w := range wantEvents {
		ev := events[i]
		if ev.EntityID != w.entityID || ev.Type != w.evType || ev.Payload["source"] != w.source || ev.Actor != "importer-test" {
			t.Errorf("events[%d] = %+v, want %s %s source %q actor importer-test", i, ev, w.entityID, w.evType, w.source)
		}
	}
}

func TestApplyContinuesIDSequence(t *testing.T) {
	repoDir := t.TempDir()
	if err := store.InitLayout(repoDir, store.Project{Name: "demo"}); err != nil {
		t.Fatalf("InitLayout: %v", err)
	}
	now := time.Now().UTC()
	existing := store.Feature{ID: "F-0001", Title: "Existente", State: domain.StateImplemented, CreatedAt: now, UpdatedAt: now}
	if err := store.SaveFeature(repoDir, existing); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}

	specPath := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(specPath, []byte("# Nova\n\nDescrição.\n"), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	cands := []Candidate{{
		Kind:   "feature",
		Title:  "Nova",
		State:  "candidate",
		Files:  map[string]string{"spec.md": specPath},
		Source: "docs/process/cases/0004_nova/",
	}}
	sum, err := Apply(repoDir, cands, "importer-test")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if sum.Features != 1 {
		t.Fatalf("Summary = %+v, want 1 feature", sum)
	}
	f, err := store.LoadFeature(repoDir, "F-0002")
	if err != nil {
		t.Fatalf("LoadFeature F-0002 (sequence must continue past F-0001): %v", err)
	}
	if f.Title != "Nova" {
		t.Fatalf("F-0002 = %+v, want title Nova", f)
	}
}
