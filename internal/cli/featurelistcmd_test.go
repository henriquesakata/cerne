package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// TestFeatureListDefault pins the flat, agent-facing listing: every non-terminal
// feature (candidate/approved/in_development/implemented) as a single line with
// the raw English state and the module marker; superseded/rejected hidden.
func TestFeatureListDefault(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedStatusFeatures(t, repoDir)

	out, err := execCerne(t, "feature", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list: %v\noutput: %s", err, out)
	}

	wantLines := []string{
		"F-0001  [approved]  Login social  · module identity",
		"F-0002  [candidate]  Exportar CSV",
		"F-0003  [in_development]  Parcelamento de boletos  · module payments",
		"F-0004  [implemented]  Relatório mensal  · module core",
	}
	for _, l := range wantLines {
		if !strings.Contains(out, l+"\n") {
			t.Fatalf("output missing line %q:\n%s", l, out)
		}
	}
	// Terminal states hidden without --all.
	for _, banned := range []string{"F-0005", "F-0006", "superseded", "rejected"} {
		if strings.Contains(out, banned) {
			t.Fatalf("default output leaks %q, want hidden:\n%s", banned, out)
		}
	}
}

// TestFeatureListAll pins --all: terminal states appear, superseded carries the
// successor marker.
func TestFeatureListAll(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedStatusFeatures(t, repoDir)

	out, err := execCerne(t, "feature", "list", "--all", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --all: %v\noutput: %s", err, out)
	}
	for _, l := range []string{
		"F-0005  [superseded]  Feature antiga  · module core · → F-0004",
		"F-0006  [rejected]  Fora de escopo  · module core",
	} {
		if !strings.Contains(out, l+"\n") {
			t.Fatalf("--all output missing line %q:\n%s", l, out)
		}
	}
}

// TestFeatureListStateFilter pins --state: exact match, and an explicit terminal
// state is shown even without --all.
func TestFeatureListStateFilter(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedStatusFeatures(t, repoDir)

	out, err := execCerne(t, "feature", "list", "--state", "in_development", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --state: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0003") || strings.Contains(out, "F-0001") || strings.Contains(out, "F-0004") {
		t.Fatalf("--state in_development should show only F-0003:\n%s", out)
	}

	// Explicit terminal state shows without --all.
	out, err = execCerne(t, "feature", "list", "--state", "superseded", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --state superseded: %v\noutput: %s", err, out)
	}
	// F-0004 legitimately appears inside F-0005's "→ F-0004" successor marker,
	// so assert it is not present as its own row ("F-0004  [").
	if !strings.Contains(out, "F-0005  [") || strings.Contains(out, "F-0004  [") {
		t.Fatalf("--state superseded should show only F-0005:\n%s", out)
	}
}

// TestFeatureListModuleFilter pins --module, ANDed with the default terminal
// hiding and with --all.
func TestFeatureListModuleFilter(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedStatusFeatures(t, repoDir)

	out, err := execCerne(t, "feature", "list", "--module", "core", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --module: %v\noutput: %s", err, out)
	}
	// Only the non-terminal core feature (F-0004); terminal core ones hidden.
	if !strings.Contains(out, "F-0004") || strings.Contains(out, "F-0005") || strings.Contains(out, "F-0006") {
		t.Fatalf("--module core (default) should show only F-0004:\n%s", out)
	}
	if strings.Contains(out, "F-0001") || strings.Contains(out, "F-0003") {
		t.Fatalf("--module core leaked a non-core feature:\n%s", out)
	}

	out, err = execCerne(t, "feature", "list", "--module", "core", "--all", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --module --all: %v\noutput: %s", err, out)
	}
	for _, id := range []string{"F-0004", "F-0005", "F-0006"} {
		if !strings.Contains(out, id) {
			t.Fatalf("--module core --all missing %s:\n%s", id, out)
		}
	}
}

// TestFeatureListParentFilter pins --parent: only the children of the epic, with
// the épico marker.
func TestFeatureListParentFilter(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0001", Title: "Épico", State: domain.StateInDevelopment},
		{ID: "F-0002", Title: "Filha A", Parent: "F-0001", State: domain.StateApproved},
		{ID: "F-0003", Title: "Filha B", Parent: "F-0001", State: domain.StateCandidate},
		{ID: "F-0004", Title: "Solta", State: domain.StateCandidate},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed epic"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "feature", "list", "--parent", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --parent: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0002  [approved]  Filha A  · epic F-0001\n") {
		t.Fatalf("--parent missing child A with epic marker:\n%s", out)
	}
	if !strings.Contains(out, "F-0003") || strings.Contains(out, "F-0004") || strings.Contains(out, "Épico\n") {
		t.Fatalf("--parent should show only the two children:\n%s", out)
	}
}

// TestFeatureListTagFilter pins the --facet/--component filters (D-0003): each
// matches on the respective axis, they AND together, and the line shows the tag
// markers.
func TestFeatureListTagFilter(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0001", Title: "Infra do gitback", State: domain.StateInDevelopment, Facets: []string{"infra"}, Components: []string{"gitback"}},
		{ID: "F-0002", Title: "Infra do índice", State: domain.StateApproved, Facets: []string{"infra"}, Components: []string{"index"}},
		{ID: "F-0003", Title: "Código puro", State: domain.StateCandidate, Facets: []string{"code"}},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed tagged"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	// --facet infra → F-0001, F-0002 (not F-0003).
	out, err := execCerne(t, "feature", "list", "--facet", "infra", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --facet: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001") || !strings.Contains(out, "F-0002") || strings.Contains(out, "F-0003") {
		t.Fatalf("--facet infra should match F-0001,F-0002 only:\n%s", out)
	}
	// The marker renders (case-normalized filter still matches).
	if !strings.Contains(out, "F-0001  [in_development]  Infra do gitback  · facet infra · comp gitback\n") {
		t.Fatalf("tag markers missing on the line:\n%s", out)
	}

	// --component gitback → only F-0001.
	out, err = execCerne(t, "feature", "list", "--component", "Gitback", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --component: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001") || strings.Contains(out, "F-0002") {
		t.Fatalf("--component gitback (case-normalized) should match only F-0001:\n%s", out)
	}

	// AND: --facet infra --component index → only F-0002.
	out, err = execCerne(t, "feature", "list", "--facet", "infra", "--component", "index", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --facet --component: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0002") || strings.Contains(out, "F-0001") || strings.Contains(out, "F-0003") {
		t.Fatalf("--facet infra --component index should match only F-0002:\n%s", out)
	}
}

// TestFeatureListOwnerAndSort pins F-0204's criterion 3: --owner filters
// case-insensitively, --sort priority orders p0..p3 first with unprioritized
// features last, and the line renders the owner/priority markers.
func TestFeatureListOwnerAndSort(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0001", Title: "Média", State: domain.StateCandidate, Owner: "ana", Priority: "p2"},
		{ID: "F-0002", Title: "Urgente", State: domain.StateCandidate, Owner: "bob", Priority: "p0"},
		{ID: "F-0003", Title: "Sem prioridade", State: domain.StateCandidate, Owner: "ana"},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed owner/priority"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	// --owner filters case-insensitively.
	out, err := execCerne(t, "feature", "list", "--owner", "Ana", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --owner: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001") || !strings.Contains(out, "F-0003") || strings.Contains(out, "F-0002") {
		t.Fatalf("--owner Ana should match F-0001,F-0003 only:\n%s", out)
	}
	if !strings.Contains(out, "F-0001  [candidate]  Média  · owner ana · priority p2\n") {
		t.Fatalf("owner/priority markers missing on the line:\n%s", out)
	}

	// --sort priority: p0 first, then p2, unset last.
	out, err = execCerne(t, "feature", "list", "--sort", "priority", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --sort priority: %v\noutput: %s", err, out)
	}
	i2 := strings.Index(out, "F-0002")
	i1 := strings.Index(out, "F-0001")
	i3 := strings.Index(out, "F-0003")
	if i2 < 0 || i1 < 0 || i3 < 0 || !(i2 < i1 && i1 < i3) {
		t.Fatalf("--sort priority order = F-0002@%d, F-0001@%d, F-0003@%d, want F-0002 < F-0001 < F-0003:\n%s", i2, i1, i3, out)
	}

	// Invalid --sort value is rejected.
	if _, err := execCerne(t, "feature", "list", "--sort", "bogus", "--dir", projDir); err == nil {
		t.Fatalf("--sort bogus succeeded, want error")
	}
}

// TestFeatureListJSON pins --json: it returns the FILTERED set (not the whole
// roster), with no human table decoration.
func TestFeatureListJSON(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedStatusFeatures(t, repoDir)

	out, err := execCerne(t, "feature", "list", "--state", "candidate", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --json: %v\noutput: %s", err, out)
	}
	var parsed struct {
		Features []store.Feature `json:"features"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if len(parsed.Features) != 1 || parsed.Features[0].ID != "F-0002" {
		t.Fatalf("--json --state candidate should return only F-0002, got %+v", parsed.Features)
	}
}

// TestFeatureListInvalidState pins the guard: an unknown --state value errors and
// names the valid set, rather than silently returning nothing.
func TestFeatureListInvalidState(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	seedStatusFeatures(t, repoDir)

	_, err := execCerne(t, "feature", "list", "--state", "dev", "--dir", projDir)
	if err == nil {
		t.Fatalf("feature list --state dev succeeded, want error")
	}
	if !strings.Contains(err.Error(), "in_development") {
		t.Fatalf("error %q should list the valid states", err)
	}
}

// TestFeatureListDone pins the F-0131 acceptance state in the flat listing: a
// done feature is visible by default (like implemented — only superseded and
// rejected are hidden), --state done filters to it, and the valid-states hint
// includes done.
func TestFeatureListDone(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	now := time.Now().UTC()
	features := []store.Feature{
		{ID: "F-0001", Title: "Pronta", State: domain.StateImplemented},
		{ID: "F-0002", Title: "Aceita", State: domain.StateDone},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(repoDir, f); err != nil {
			t.Fatalf("SaveFeature %s: %v", f.ID, err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed done"); err != nil {
		t.Fatalf("CommitAndPush: %v", err)
	}

	out, err := execCerne(t, "feature", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0002  [done]  Aceita\n") {
		t.Fatalf("done feature hidden in default listing:\n%s", out)
	}

	out, err = execCerne(t, "feature", "list", "--state", "done", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list --state done: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0002  [") || strings.Contains(out, "F-0001  [") {
		t.Fatalf("--state done should show only F-0002:\n%s", out)
	}

	_, err = execCerne(t, "feature", "list", "--state", "nope", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "done") {
		t.Fatalf("valid-states hint should include done, got: %v", err)
	}
}

// TestFeatureListEmpty pins the two empty cases: no features at all vs. filters
// matching nothing.
func TestFeatureListEmpty(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	out, err := execCerne(t, "feature", "list", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list (empty): %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "No features yet") {
		t.Fatalf("empty project message missing:\n%s", out)
	}

	seedStatusFeatures(t, repoDir)
	out, err = execCerne(t, "feature", "list", "--module", "inexistente", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature list (no match): %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "No features match the filters") {
		t.Fatalf("no-match message missing:\n%s", out)
	}
}
