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
	if !strings.Contains(out, "F-0001 created (candidate)") {
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
	if !strings.Contains(out, "F-0002 created (candidate)") {
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
	// F-0235 crit.1/5: the parent's plan is scaffolded right away (candidate,
	// no approve yet) and the CLI advises filling it.
	if !strings.Contains(out, "plano do épico scaffoldado — preencha antes do approve (N-0031)") {
		t.Fatalf("output missing epic-scaffold advisory:\n%s", out)
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

// TestFeatureNewFileFrontmatter (F-0328 crit. 5): `feature new --file` shares
// the spec rule — a block naming the id being minted is the one block kept,
// extra keys and all; another feature's id is refused before anything is
// minted: no entity, no spec dir, origin unmoved.
func TestFeatureNewFileFrontmatter(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	specFile := filepath.Join(t.TempDir(), "spec.md")
	own := "---\ncerne_id: F-0001\nx: y\n---\n\n# Título\n\n## Problema\n\nCom bloco próprio.\n"
	mustWrite(t, specFile, own)
	if out, err := execCerne(t, "feature", "new", "Título", "--file", specFile, "--dir", projDir); err != nil {
		t.Fatalf("feature new --file: %v\noutput: %s", err, out)
	}
	if spec, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "spec.md")); string(spec) != own {
		t.Fatalf("spec.md = %q, want the body's own single block %q", spec, own)
	}

	head0, _ := gitback.HeadSHA(origin)
	mustWrite(t, specFile, "---\ncerne_id: F-0009\n---\n\n# Outra\n\n## Problema\n\nDe outra feature.\n")
	_, err := execCerne(t, "feature", "new", "Exportar relatório em PDF", "--file", specFile, "--dir", projDir)
	// F-0002 was never minted: the refusal names only the body's id and sends
	// nobody to `cerne show` a feature that does not exist (critic C4).
	if !errors.Is(err, app.ErrBodyFrontmatter) || !strings.Contains(err.Error(), "the body carries cerne_id F-0009 — a new feature's spec cannot carry another feature's frontmatter") ||
		strings.Contains(err.Error(), "F-0002") || strings.Contains(err.Error(), "cerne show") {
		t.Fatalf("err = %v, want ErrBodyFrontmatter naming only F-0009, no show hint", err)
	}
	mustWrite(t, specFile, "---\ntitle: x\n---\n\n# Outra\n\n## Problema\n\nSem id.\n")
	if _, err := execCerne(t, "feature", "new", "Exportar relatório em PDF", "--file", specFile, "--dir", projDir); !errors.Is(err, app.ErrBodyFrontmatter) ||
		!strings.Contains(err.Error(), "without cerne_id") || strings.Contains(err.Error(), "cerne show") {
		t.Fatalf("block without id: err = %v, want a refusal with no show hint", err)
	}
	if features, _ := store.LoadAllFeatures(repoDir); len(features) != 1 {
		t.Fatalf("features = %d, want 1 (a refused body must not mint)", len(features))
	}
	if _, err := os.Stat(filepath.Join(repoDir, "content", "features", "F-0002")); !os.IsNotExist(err) {
		t.Fatalf("content/features/F-0002 exists after a refusal (stat err %v)", err)
	}
	if h, _ := gitback.HeadSHA(origin); h != head0 {
		t.Fatalf("origin moved on a refusal: %s -> %s", head0, h)
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
	if !strings.Contains(out, "✓ F-0001 created (candidate) with spec — already synced") {
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
	if !strings.Contains(out, "F-0001 created (candidate)") {
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
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
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
	if !strings.Contains(out, "F-0001 retagged") {
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

// TestFeatureNewOwnerPriority pins F-0204's criterion 1 at the CLI: --owner
// and --priority persist, and an invalid priority is refused before anything
// is minted.
func TestFeatureNewOwnerPriority(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	out, err := execCerne(t, "feature", "new", "Feat", "--context", "ctx de teste",
		"--owner", "ana", "--priority", "p1", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature new: %v\noutput: %s", err, out)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.Owner != "ana" || f.Priority != "p1" {
		t.Fatalf("feature = %+v, want Owner=ana Priority=p1", f)
	}

	if _, err := execCerne(t, "feature", "new", "Feat 2", "--context", "ctx de teste",
		"--priority", "p9", "--dir", projDir); err == nil {
		t.Fatalf("--priority p9 succeeded, want error")
	}
	if _, err := store.LoadFeature(repoDir, "F-0002"); err == nil {
		t.Fatalf("F-0002 minted despite invalid --priority")
	}
}

// TestFeatureSet pins F-0204's criterion 2 at the CLI: 'feature set' mutates
// owner/priority in place, the no-flags guard, priority validation, and the
// terminal-state gate.
func TestFeatureSet(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Feat", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}

	// set with no flag is a usage error.
	if _, err := execCerne(t, "feature", "set", "F-0001", "--dir", projDir); err == nil {
		t.Fatalf("set with no flags succeeded, want error")
	}

	out, err := execCerne(t, "feature", "set", "F-0001", "--owner", "ana", "--priority", "p0", "--dir", projDir)
	if err != nil {
		t.Fatalf("feature set: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "F-0001 set") {
		t.Fatalf("output missing confirmation:\n%s", out)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if f.Owner != "ana" || f.Priority != "p0" {
		t.Fatalf("feature = %+v, want Owner=ana Priority=p0", f)
	}

	// Invalid priority: refused, nothing changed.
	if _, err := execCerne(t, "feature", "set", "F-0001", "--priority", "urgent", "--dir", projDir); err == nil {
		t.Fatalf("--priority urgent succeeded, want error")
	}
	f, err = store.LoadFeature(repoDir, "F-0001")
	if err != nil || f.Priority != "p0" {
		t.Fatalf("Priority = %q, %v, want unchanged p0", f.Priority, err)
	}

	// Terminal feature is frozen.
	frozen := store.Feature{ID: "F-0002", Title: "Pronta", State: domain.StateImplemented}
	if err := store.SaveFeature(repoDir, frozen); err != nil {
		t.Fatalf("SaveFeature frozen: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed F-0002"); err != nil {
		t.Fatalf("CommitAndPush seed: %v", err)
	}
	if _, err := execCerne(t, "feature", "set", "F-0002", "--owner", "ana", "--dir", projDir); err == nil {
		t.Fatalf("set on implemented feature succeeded, want error")
	}
}

// TestFeatureFixesFlag pins F-0321's criteria 17-18 at the CLI: --fixes on
// feature new and feature set writes the link, an empty set clears it, an
// unknown target is refused with nothing written, and set with no flag names
// --fixes among the options.
func TestFeatureFixesFlag(t *testing.T) {
	projDir, _, repoDir := setupProject(t)
	if _, err := execCerne(t, "feature", "new", "Entregue", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v", err)
	}
	if out, err := execCerne(t, "feature", "new", "Fix", "--context", "ctx de teste", "--fixes", "F-0001", "--dir", projDir); err != nil {
		t.Fatalf("feature new --fixes: %v\noutput: %s", err, out)
	}
	if f, err := store.LoadFeature(repoDir, "F-0002"); err != nil || f.Fixes != "F-0001" {
		t.Fatalf("F-0002 fixes = %q, %v, want F-0001", f.Fixes, err)
	}
	if out, err := execCerne(t, "feature", "set", "F-0002", "--fixes", "", "--dir", projDir); err != nil {
		t.Fatalf("feature set --fixes \"\": %v\noutput: %s", err, out)
	}
	if f, _ := store.LoadFeature(repoDir, "F-0002"); f.Fixes != "" {
		t.Fatalf("F-0002 fixes = %q after clear, want empty", f.Fixes)
	}
	if _, err := execCerne(t, "feature", "new", "Outro", "--context", "ctx de teste", "--fixes", "F-9999", "--dir", projDir); err == nil {
		t.Fatalf("feature new --fixes F-9999 succeeded, want error")
	}
	if _, err := store.LoadFeature(repoDir, "F-0003"); err == nil {
		t.Fatalf("F-0003 persisted despite the unknown --fixes target")
	}
	_, err := execCerne(t, "feature", "set", "F-0002", "--dir", projDir)
	if err == nil || !strings.Contains(err.Error(), "--fixes") {
		t.Fatalf("set with no flag = %v, want an error naming --fixes", err)
	}
}
