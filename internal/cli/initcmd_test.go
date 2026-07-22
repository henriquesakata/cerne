package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/store"
)

// execInit executes "cerne init" with args, returning combined output.
func execInit(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := Root()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(append([]string{"init"}, args...))
	err := root.Execute()
	return out.String(), err
}

// newOrigin creates a local bare backend repo.
func newOrigin(t *testing.T) string {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	return origin
}

func TestInitFresh(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cacheRoot)
	origin := newOrigin(t)
	projDir := t.TempDir()

	out, err := execInit(t,
		"--name", "My Project", "--backend", origin,
		"--module", "core", "--module", "web", "--dir", projDir)
	if err != nil {
		t.Fatalf("init: %v\noutput: %s", err, out)
	}

	cfg, foundDir, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ProjectID != "my-project" {
		t.Fatalf("ProjectID = %q, want %q", cfg.ProjectID, "my-project")
	}
	if cfg.Backend != origin {
		t.Fatalf("Backend = %q, want %q", cfg.Backend, origin)
	}
	if foundDir != projDir {
		t.Fatalf("config dir = %q, want %q", foundDir, projDir)
	}

	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if lock.Version != 1 || lock.Backend != origin {
		t.Fatalf("lock = %+v, want version 1 backend %q", lock, origin)
	}
	manifest, err := project.LoadManifest(projDir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(manifest) != 0 {
		t.Fatalf("manifest = %v, want empty after init", manifest)
	}

	repoDir := filepath.Join(cacheRoot, "my-project", "repo")
	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	if head == "" || lock.Snapshot != head {
		t.Fatalf("lock.Snapshot = %q, want backend HEAD %q", lock.Snapshot, head)
	}

	p, err := store.LoadProject(repoDir)
	if err != nil {
		t.Fatalf("LoadProject in backend clone: %v", err)
	}
	if p.Name != "My Project" || len(p.Modules) != 2 || p.Modules[0] != "core" || p.Modules[1] != "web" {
		t.Fatalf("backend project = %+v, want name %q modules [core web]", p, "My Project")
	}

	if _, err := os.Stat(filepath.Join(projDir, ".docs", ".gitkeep")); err != nil {
		t.Fatalf(".docs/.gitkeep: %v", err)
	}
	if !strings.Contains(out, ".gitignore") {
		t.Fatalf("output missing .gitignore next step:\n%s", out)
	}
}

func TestInitDoubleInitFails(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := newOrigin(t)
	projDir := t.TempDir()

	if out, err := execInit(t, "--name", "demo", "--backend", origin, "--dir", projDir); err != nil {
		t.Fatalf("first init: %v\noutput: %s", err, out)
	}
	_, err := execInit(t, "--name", "demo", "--backend", origin, "--dir", projDir)
	if err == nil {
		t.Fatalf("second init succeeded, want error")
	}
	if !strings.Contains(err.Error(), projDir) {
		t.Fatalf("error %q does not say where .cernerc was found (%s)", err, projDir)
	}

	// Same error from a nested subdir: LoadConfig semantics walk up.
	nested := filepath.Join(projDir, "src", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	_, err = execInit(t, "--name", "demo", "--backend", origin, "--dir", nested)
	if err == nil || !strings.Contains(err.Error(), projDir) {
		t.Fatalf("init in nested dir err = %v, want error naming %s", err, projDir)
	}
}

func TestInitJoinExistingBackendFreshCache(t *testing.T) {
	cache1 := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cache1)
	origin := newOrigin(t)
	dir1 := t.TempDir()
	if out, err := execInit(t, "--name", "demo", "--backend", origin, "--module", "core", "--dir", dir1); err != nil {
		t.Fatalf("first init: %v\noutput: %s", err, out)
	}
	lock1, err := project.LoadLock(dir1)
	if err != nil {
		t.Fatalf("LoadLock dir1: %v", err)
	}

	// Second project dir, fresh cache: simulates joining from another machine.
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	dir2 := t.TempDir()
	out, err := execInit(t, "--name", "demo", "--backend", origin, "--dir", dir2)
	if err != nil {
		t.Fatalf("join init: %v\noutput: %s", err, out)
	}
	lock2, err := project.LoadLock(dir2)
	if err != nil {
		t.Fatalf("LoadLock dir2: %v", err)
	}
	if lock2.Snapshot != lock1.Snapshot {
		t.Fatalf("join lock.Snapshot = %q, want %q", lock2.Snapshot, lock1.Snapshot)
	}
	if _, _, err := project.LoadConfig(dir2); err != nil {
		t.Fatalf("LoadConfig dir2: %v", err)
	}
}

func TestInitJoinExistingBackendSharedCache(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := newOrigin(t)
	dir1 := t.TempDir()
	if out, err := execInit(t, "--name", "demo", "--backend", origin, "--dir", dir1); err != nil {
		t.Fatalf("first init: %v\noutput: %s", err, out)
	}

	// Same machine, same cache: the backend clone already exists.
	dir2 := t.TempDir()
	out, err := execInit(t, "--name", "demo", "--backend", origin, "--dir", dir2)
	if err != nil {
		t.Fatalf("join init with shared cache: %v\noutput: %s", err, out)
	}
	lock2, err := project.LoadLock(dir2)
	if err != nil {
		t.Fatalf("LoadLock dir2: %v", err)
	}
	if lock2.Snapshot == "" {
		t.Fatalf("join lock.Snapshot empty, want backend HEAD")
	}
}

func TestInitInstallsSkills(t *testing.T) {
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	origin := newOrigin(t)
	projDir := t.TempDir()

	out, err := execInit(t, "--name", "demo", "--backend", origin, "--dir", projDir)
	if err != nil {
		t.Fatalf("init: %v\noutput: %s", err, out)
	}

	skillFile := filepath.Join(projDir, ".claude", "skills", "cerne-authoring", "SKILL.md")
	if _, err := os.Stat(skillFile); err != nil {
		t.Fatalf("expected canonical skill installed at %s: %v", skillFile, err)
	}
	if !strings.Contains(out, "cerne-authoring: created in .claude/skills/cerne-authoring") {
		t.Fatalf("output missing skill install status:\n%s", out)
	}
}

// TestInitRejoinDirtyCacheCloneDoesNotPoison pins the N-0027 poison at init's
// cache-refresh: joining a project from a second workspace while the shared
// cache clone holds an uncommitted write (a CLI synthesis in flight in another
// workspace) must neither fail nor use raw Pull — which moves refs/heads/main
// and then aborts, leaving ref and files diverged. The refresh is skipped and
// init proceeds on the cached state.
func TestInitRejoinDirtyCacheCloneDoesNotPoison(t *testing.T) {
	_, origin, repoDir := setupProject(t)

	notes := filepath.Join(repoDir, "notes.md")
	if err := os.WriteFile(notes, []byte("notes v1\n"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "add notes"); err != nil {
		t.Fatalf("CommitAndPush notes: %v", err)
	}

	// A teammate advances the remote.
	mate := filepath.Join(t.TempDir(), "mate")
	if err := gitback.Clone(origin, mate); err != nil {
		t.Fatalf("Clone mate: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mate, "wiki.md"), []byte("wiki v2\n"), 0o644); err != nil {
		t.Fatalf("write mate wiki: %v", err)
	}
	if _, err := gitback.CommitAndPush(mate, "remote update"); err != nil {
		t.Fatalf("CommitAndPush mate: %v", err)
	}

	// Poison window: an uncommitted write in the shared clone.
	if err := os.WriteFile(notes, []byte("notes: synthesis in flight\n"), 0o644); err != nil {
		t.Fatalf("poison notes: %v", err)
	}
	baseline, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA baseline: %v", err)
	}

	projDir2 := t.TempDir()
	out, err := execInit(t, "--name", "demo", "--backend", origin, "--dir", projDir2)
	if err != nil {
		t.Fatalf("init over a dirty cache clone must succeed on the cached state, got: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Joined existing backend") {
		t.Fatalf("output missing join confirmation:\n%s", out)
	}

	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA after init: %v", err)
	}
	if head != baseline {
		t.Fatalf("init moved HEAD %s -> %s while the worktree was uncommitted: ref diverged from files", baseline[:8], head[:8])
	}
	if got, _ := os.ReadFile(notes); string(got) != "notes: synthesis in flight\n" {
		t.Fatalf("init clobbered the uncommitted write: notes.md = %q", got)
	}
}
