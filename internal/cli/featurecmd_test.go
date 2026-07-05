package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// execCerne executes the root command with args, returning combined output.
func execCerne(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := Root()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// setupProject inits a cerne project against a fresh local bare origin and
// returns the project dir, the origin path and the cached backend clone dir.
func setupProject(t *testing.T, modules ...string) (projDir, origin, repoDir string) {
	t.Helper()
	cacheRoot := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cacheRoot)
	origin = newOrigin(t)
	projDir = t.TempDir()
	args := []string{"--name", "demo", "--backend", origin, "--dir", projDir}
	for _, m := range modules {
		args = append(args, "--module", m)
	}
	if out, err := execInit(t, args...); err != nil {
		t.Fatalf("init: %v\noutput: %s", err, out)
	}
	return projDir, origin, filepath.Join(cacheRoot, "demo", "repo")
}

func TestFeatureNew(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	lockBefore, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock before: %v", err)
	}

	out, err := execCerne(t, "feature", "new", "Login com Google", "--context", "ctx de teste", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001 criada (candidate)") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}

	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.Title != "Login com Google" || f.Module != "" || f.State != domain.StateCandidate {
		t.Fatalf("feature = %+v, want title %q module %q state candidate", f, "Login com Google", "")
	}
	if f.CreatedAt.IsZero() || !f.UpdatedAt.Equal(f.CreatedAt) {
		t.Fatalf("timestamps = created %v updated %v, want equal non-zero", f.CreatedAt, f.UpdatedAt)
	}

	spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("spec stub: %v", err)
	}
	for _, want := range []string{"cerne_id: F-0001", "# Login com Google", "## Problema", "## Verificação"} {
		if !strings.Contains(string(spec), want) {
			t.Fatalf("spec stub missing %q:\n%s", want, spec)
		}
	}
	if strings.Contains(string(spec), "> Spec pendente") {
		t.Fatalf("spec stub still uses the legacy one-liner:\n%s", spec)
	}

	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.Type != "feature.created" || ev.EntityID != "F-0001" || ev.Actor != "local" {
		t.Fatalf("event = %+v, want feature.created on F-0001 by local", ev)
	}
	if ev.Payload["title"] != "Login com Google" {
		t.Fatalf("event payload = %v, want title key", ev.Payload)
	}

	lockAfter, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock after: %v", err)
	}
	if lockAfter.Snapshot == lockBefore.Snapshot {
		t.Fatalf("lock.Snapshot unchanged (%s), want new backend HEAD", lockAfter.Snapshot)
	}
	originHead, err := gitback.HeadSHA(origin)
	if err != nil {
		t.Fatalf("HeadSHA origin: %v", err)
	}
	if lockAfter.Snapshot != originHead {
		t.Fatalf("lock.Snapshot = %s, want pushed origin HEAD %s", lockAfter.Snapshot, originHead)
	}

	// Second feature gets the next sequential ID and the actor from CERNE_ACTOR.
	t.Setenv("CERNE_ACTOR", "henrique")
	out, err = execCerne(t, "feature", "new", "Exportar CSV", "--context", "ctx de teste", "--dir", projDir)
	if err != nil {
		t.Fatalf("second feature new: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0002 criada (candidate)") {
		t.Fatalf("second output missing F-0002:\n%s", out)
	}
	events, err = store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents after second: %v", err)
	}
	if len(events) != 2 || events[1].Actor != "henrique" {
		t.Fatalf("events = %+v, want second actor henrique", events)
	}
}

func TestFeatureNewWithParent(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Épico", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("create epic: %v", err)
	}
	out, err := execCerne(t, "feature", "new", "Filha", "--context", "ctx de teste", "--parent", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("create child: %v\noutput: %s", err, out)
	}
	f, err := store.LoadFeature(repoDir, "F-0002")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.Parent != "F-0001" {
		t.Fatalf("Parent = %q, want F-0001", f.Parent)
	}
	if _, err := execCerne(t, "feature", "new", "X", "--context", "ctx de teste", "--parent", "F-9999", "--dir", projDir); err == nil {
		t.Fatalf("feature new with unknown parent succeeded, want error")
	}
}

func TestFeatureNewModuleRules(t *testing.T) {
	tests := []struct {
		name       string
		modules    []string
		moduleFlag string
		wantErr    string // empty = success expected
		wantModule string
	}{
		{"two modules require flag", []string{"core", "web"}, "", "--module", ""},
		{"two modules unknown flag", []string{"core", "web"}, "api", "api", ""},
		{"two modules valid flag", []string{"core", "web"}, "core", "", "core"},
		{"one module flag optional", []string{"core"}, "", "", ""},
		{"one module valid flag", []string{"core"}, "core", "", "core"},
		{"one module unknown flag", []string{"core"}, "api", "api", ""},
		{"no modules flag optional", nil, "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projDir, _, repoDir := setupProject(t, tt.modules...)
			args := []string{"feature", "new", "Feature X", "--context", "ctx de teste", "--dir", projDir}
			if tt.moduleFlag != "" {
				args = append(args, "--module", tt.moduleFlag)
			}
			out, err := execCerne(t, args...)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("feature new succeeded, want error mentioning %q\noutput: %s", tt.wantErr, out)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not mention %q", err, tt.wantErr)
				}
				if _, err := store.LoadFeature(repoDir, "F-0001"); err == nil {
					t.Fatalf("feature persisted despite module validation error")
				}
				return
			}
			if err != nil {
				t.Fatalf("feature new: %v\noutput: %s", err, out)
			}
			f, err := store.LoadFeature(repoDir, "F-0001")
			if err != nil {
				t.Fatalf("LoadFeature: %v", err)
			}
			if f.Module != tt.wantModule {
				t.Fatalf("Module = %q, want %q", f.Module, tt.wantModule)
			}
		})
	}
}

func TestFeatureNewEmptyTitle(t *testing.T) {
	projDir, _, _ := setupProject(t)
	_, err := execCerne(t, "feature", "new", "   ", "--context", "ctx de teste", "--dir", projDir)
	if err == nil {
		t.Fatalf("feature new with blank title succeeded, want error")
	}
}

func TestFeatureNewOutsideProject(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	dir := t.TempDir()
	_, err := execCerne(t, "feature", "new", "Feature X", "--context", "ctx de teste", "--dir", dir)
	if err == nil {
		t.Fatalf("feature new outside a project succeeded, want error")
	}
}

// TestFeatureNewTags pins the --facet/--component flags on feature new (D-0003):
// CSV parsed, normalized, and persisted; an unknown facet errors.
func TestFeatureNewTags(t *testing.T) {
	projDir, _, repoDir := setupProject(t)

	if _, err := execCerne(t, "feature", "new", "Feat", "--context", "ctx de teste", "--facet", "infra,code", "--component", "Gitback", "--dir", projDir); err != nil {
		t.Fatalf("feature new with tags: %v", err)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if len(f.Facets) != 2 || f.Facets[0] != "infra" || f.Facets[1] != "code" {
		t.Fatalf("Facets = %v, want [infra code]", f.Facets)
	}
	if len(f.Components) != 1 || f.Components[0] != "gitback" {
		t.Fatalf("Components = %v, want [gitback]", f.Components)
	}

	if _, err := execCerne(t, "feature", "new", "Bad", "--context", "ctx de teste", "--facet", "bogus", "--dir", projDir); err == nil {
		t.Fatalf("feature new with unknown facet succeeded, want error")
	}
}

// TestFeatureNewOneShotSpec pins the one-shot path (F-0120 criterion 1):
// --file births the candidate with the real spec committed and pushed —
// nothing left to fill or push.
func TestFeatureNewOneShotSpec(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	specFile := filepath.Join(t.TempDir(), "spec.md")
	body := "# Título\n\n## Problema\n\nContexto real da criação.\n"
	if err := os.WriteFile(specFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execCerne(t, "feature", "new", "Título", "--file", specFile, "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new --file: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "✓ F-0001 criada (candidate) com spec — já sincronizada") {
		t.Fatalf("output missing one-shot confirmation:\n%s", out)
	}

	spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	want := "---\ncerne_id: F-0001\n---\n\n" + body
	if string(spec) != want {
		t.Fatalf("spec.md = %q, want stamped one-shot body %q", spec, want)
	}

	ds, err := app.FeatureDocSet(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("FeatureDocSet: %v", err)
	}
	if !ds.HasSpec {
		t.Fatalf("one-shot spec should count as real content (approve gate open)")
	}
}

// TestFeatureNewContextSeed pins the minimal path (F-0120 criterion 2):
// --context seeds the Problema section of the scaffolded spec, keeping the
// rest of the template for the author.
func TestFeatureNewContextSeed(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	ctx := "Agentes abrem a feature e não voltam para escrever a spec."

	out, err := execCerne(t, "feature", "new", "Captura", "--context", ctx, "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new --context: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001 criada (candidate)") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}

	spec, err := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md"))
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	s := string(spec)
	problema := strings.Index(s, "## Problema")
	seeded := strings.Index(s, ctx)
	objetivo := strings.Index(s, "## Objetivo")
	if seeded < 0 || !(problema < seeded && seeded < objetivo) {
		t.Fatalf("context should be seeded inside Problema (problema=%d seeded=%d objetivo=%d):\n%s", problema, seeded, objetivo, s)
	}
	for _, sec := range []string{"## Decisões do design", "## Desenho", "## Verificação", "## Fora de escopo"} {
		if !strings.Contains(s, sec) {
			t.Fatalf("seeded spec lost template section %q:\n%s", sec, s)
		}
	}
}

// TestFeatureNewRequiresContext pins the hard gate (F-0120 criterion 3): a
// bare create is refused with the instructive sentinel and nothing reaches
// the backend.
func TestFeatureNewRequiresContext(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	_, err := execCerne(t, "feature", "new", "Nua", "--dir", projDir)
	if !errors.Is(err, errFeatureContextRequired) {
		t.Fatalf("err = %v, want errFeatureContextRequired", err)
	}
	if !strings.Contains(err.Error(), "--context") {
		t.Fatalf("error should teach the minimal path (--context): %v", err)
	}
	if _, err := store.LoadFeature(repoDir, "F-0001"); err == nil {
		t.Fatalf("feature persisted despite the context gate")
	}
}

// TestFeatureNewFileAndContextExclusive pins the exclusivity rule: a full
// spec body and a context seed are different intents — both at once is a
// usage error, and nothing is created.
func TestFeatureNewFileAndContextExclusive(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	specFile := filepath.Join(t.TempDir(), "spec.md")
	if err := os.WriteFile(specFile, []byte("corpo"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := execCerne(t, "feature", "new", "X", "--file", specFile, "--context", "y", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "mutuamente exclusivos") {
		t.Fatalf("err = %v, want mutual-exclusion error", err)
	}
	if _, err := store.LoadFeature(repoDir, "F-0001"); err == nil {
		t.Fatalf("feature persisted despite the exclusivity error")
	}
}

// TestFeatureRetag pins the feature retag command: per-axis update, the
// no-flags guard, and persistence.
func TestFeatureRetag(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Feat", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}

	// retag with no axis flag is a usage error.
	if _, err := execCerne(t, "feature", "retag", "F-0001", "--dir", projDir); err == nil {
		t.Fatalf("retag with no flags succeeded, want error")
	}

	out, err := execCerne(t, "feature", "retag", "F-0001", "--component", "gitback", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature retag: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001 retaggeada") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if len(f.Components) != 1 || f.Components[0] != "gitback" {
		t.Fatalf("Components = %v, want [gitback]", f.Components)
	}
}
