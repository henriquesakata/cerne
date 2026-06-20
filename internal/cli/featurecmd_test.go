package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	out, err := execCerne(t, "feature", "new", "Login com Google", "--dir", projDir)
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
	out, err = execCerne(t, "feature", "new", "Exportar CSV", "--dir", projDir)
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
	if _, err := execCerne(t, "feature", "new", "Épico", "--dir", projDir); err != nil {
		t.Fatalf("create epic: %v", err)
	}
	out, err := execCerne(t, "feature", "new", "Filha", "--parent", "F-0001", "--dir", projDir)
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
	if _, err := execCerne(t, "feature", "new", "X", "--parent", "F-9999", "--dir", projDir); err == nil {
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
			args := []string{"feature", "new", "Feature X", "--dir", projDir}
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
	_, err := execCerne(t, "feature", "new", "   ", "--dir", projDir)
	if err == nil {
		t.Fatalf("feature new with blank title succeeded, want error")
	}
}

func TestFeatureNewOutsideProject(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	dir := t.TempDir()
	_, err := execCerne(t, "feature", "new", "Feature X", "--dir", dir)
	if err == nil {
		t.Fatalf("feature new outside a project succeeded, want error")
	}
}
