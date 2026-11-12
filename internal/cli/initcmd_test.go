package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
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

// TestInitRelativeLocalBackend pins F-0335 criterion 1: a relative local
// backend is resolved against the cwd before cloning. Stored verbatim, it
// broke the layout push — git runs inside the cached clone, where the
// relative path points elsewhere.
func TestInitRelativeLocalBackend(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cacheRoot)
	parent := t.TempDir()
	if err := gitback.InitBare(filepath.Join(parent, "origin.git")); err != nil {
		t.Fatalf("InitBare: %v", err)
	}
	projDir := filepath.Join(parent, "proj")
	if err := os.Mkdir(projDir, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	t.Chdir(projDir)

	out, err := execInit(t, "--name", "demo", "--backend", "../origin.git", "--dir", projDir)
	if err != nil {
		t.Fatalf("init with a relative backend: %v\noutput: %s", err, out)
	}
	want, err := filepath.Abs("../origin.git")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Backend != want {
		t.Fatalf(".cernerc backend = %q, want %q", cfg.Backend, want)
	}
	lock, err := project.LoadLock(projDir)
	if err != nil {
		t.Fatalf("LoadLock: %v", err)
	}
	if lock.Backend != want {
		t.Fatalf("docs.lock backend = %q, want %q", lock.Backend, want)
	}
	if got, err := gitback.OriginURL(filepath.Join(cacheRoot, "demo", "repo")); err != nil || got != want {
		t.Fatalf("cached clone origin = %q (err %v), want %q", got, err, want)
	}
}

// TestInitFailureRemovesFreshClone pins F-0335 criterion 2: an init that
// fails after cloning removes the clone it created, so the retry starts from
// a fresh clone instead of refreshing the dead one (or "joining" a layout that
// was committed locally but never published).
func TestInitFailureRemovesFreshClone(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cacheRoot)
	// A non-bare empty repo clones fine but refuses the layout push
	// (receive.denyCurrentBranch): a failure after the clone exists.
	nonBare := filepath.Join(t.TempDir(), "nonbare")
	if out, err := exec.Command("git", "init", "-q", "-b", "main", nonBare).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	projDir := t.TempDir()
	if out, err := execInit(t, "--name", "demo", "--backend", nonBare, "--dir", projDir); err == nil {
		t.Fatalf("init against a push-refusing backend must fail\noutput: %s", out)
	}
	repoDir := filepath.Join(cacheRoot, "demo", "repo")
	if _, err := os.Stat(repoDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed init left its clone at %s (stat err: %v)", repoDir, err)
	}

	origin := newOrigin(t)
	if out, err := execInit(t, "--name", "demo", "--backend", origin, "--dir", projDir); err != nil {
		t.Fatalf("retry after a failed init: %v\noutput: %s", err, out)
	}
	if got, err := gitback.OriginURL(repoDir); err != nil || got != origin {
		t.Fatalf("retry clone origin = %q (err %v), want %q", got, err, origin)
	}
}

// TestInitCachedCloneOtherBackendRefuses pins F-0335 criterion 3: the cache
// is keyed by project id alone, so a second project with the same name and
// another backend used to "join" the first one's backend silently. Now it is
// refused, and the existing clone — possibly a live project's — is left alone.
func TestInitCachedCloneOtherBackendRefuses(t *testing.T) {
	cacheRoot := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cacheRoot)
	originA := newOrigin(t)
	if out, err := execInit(t, "--name", "demo", "--backend", originA, "--dir", t.TempDir()); err != nil {
		t.Fatalf("first init: %v\noutput: %s", err, out)
	}

	originB := newOrigin(t)
	dir2 := t.TempDir()
	out, err := execInit(t, "--name", "demo", "--backend", originB, "--dir", dir2)
	if !errors.Is(err, ErrCacheBackendMismatch) {
		t.Fatalf("init over a cached clone of another backend: err = %v, want ErrCacheBackendMismatch\noutput: %s", err, out)
	}
	if _, _, err := project.LoadConfig(dir2); !errors.Is(err, project.ErrNoProject) {
		t.Fatalf("refused init wrote .cernerc in %s (LoadConfig err: %v)", dir2, err)
	}
	if got, err := gitback.OriginURL(filepath.Join(cacheRoot, "demo", "repo")); err != nil || got != originA {
		t.Fatalf("cached clone origin = %q (err %v), want it untouched at %q", got, err, originA)
	}
}

// localBackendEnv isolates the data and cache roots; the data root contains a
// space on purpose (macOS "Application Support").
func localBackendEnv(t *testing.T) (dataRoot, cacheRoot string) {
	t.Helper()
	dataRoot = filepath.Join(t.TempDir(), "data root")
	cacheRoot = t.TempDir()
	t.Setenv("CERNE_DATA_DIR", dataRoot)
	t.Setenv("CERNE_CACHE_DIR", cacheRoot)
	return dataRoot, cacheRoot
}

func TestInit_NoBackendCreatesLocal(t *testing.T) {
	dataRoot, _ := localBackendEnv(t)
	dir := t.TempDir()
	out, err := execInit(t, "--name", "Solo", "--dir", dir)
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	bare := filepath.Join(dataRoot, "backends", "solo.git")
	if _, err := os.Stat(filepath.Join(bare, "HEAD")); err != nil {
		t.Fatalf("bare backend not created at %s: %v", bare, err)
	}
	cfg, _, err := project.LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backend != bare {
		t.Fatalf("Backend = %q, want %q", cfg.Backend, bare)
	}
	if !strings.Contains(out, bare) {
		t.Fatalf("output does not print the created path %s:\n%s", bare, out)
	}
}

func TestInit_LocalBackendOutsideProjectAndCache(t *testing.T) {
	_, cacheRoot := localBackendEnv(t)
	dir := t.TempDir()
	if out, err := execInit(t, "--name", "solo", "--dir", dir); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	cfg, _, err := project.LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(cfg.Backend, dir) || strings.HasPrefix(cfg.Backend, cacheRoot) {
		t.Fatalf("backend %s lives inside the project %s or the cache %s", cfg.Backend, dir, cacheRoot)
	}
	// Deleting the cache must not lose the backend.
	if err := os.RemoveAll(cacheRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.Backend); err != nil {
		t.Fatalf("backend gone after deleting the cache: %v", err)
	}
}

func TestInit_NoBackendJoinsExistingLocal(t *testing.T) {
	dataRoot, _ := localBackendEnv(t)
	first, second := t.TempDir(), t.TempDir()
	if out, err := execInit(t, "--name", "solo", "--dir", first); err != nil {
		t.Fatalf("first init: %v\n%s", err, out)
	}
	out, err := execInit(t, "--name", "solo", "--dir", second)
	if err != nil {
		t.Fatalf("second init: %v\n%s", err, out)
	}
	bare := filepath.Join(dataRoot, "backends", "solo.git")
	if !strings.Contains(out, "Joined existing backend "+bare) {
		t.Fatalf("second init did not join %s:\n%s", bare, out)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "backends", "solo-2.git")); err == nil {
		t.Fatal("a second backend was created")
	}
}

func TestInit_NoBackendPrintsHint(t *testing.T) {
	localBackendEnv(t)
	out, err := execInit(t, "--name", "solo", "--dir", t.TempDir())
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Backend is local:") || !strings.Contains(out, "push --mirror") || !strings.Contains(out, ".cernerc") || !strings.Contains(out, "then delete") {
		t.Fatalf("missing local-backend hint:\n%s", out)
	}
	// An explicit --backend keeps today's output: no hint.
	out, err = execInit(t, "--name", "other", "--backend", newOrigin(t), "--dir", t.TempDir())
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if strings.Contains(out, "Backend is local") {
		t.Fatalf("hint printed with explicit --backend:\n%s", out)
	}
}

// TestInit_WritesLightMode (F-0364, Ajuste 1): a new project starts without
// the Gauntlet — [gates] gauntlet = "off" in .cernerc — and init invites the
// user to turn it on, with --backend or without.
func TestInit_WritesLightMode(t *testing.T) {
	const invite = `Gauntlet is off (light mode). Turn it on any time: set gauntlet = "on" under [gates] in .cernerc.`
	localBackendEnv(t)
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	for name, extra := range map[string][]string{"local": nil, "with --backend": {"--backend", newOrigin(t)}} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			out, err := execInit(t, append([]string{"--name", "Leve " + name, "--dir", dir}, extra...)...)
			if err != nil {
				t.Fatalf("init: %v\n%s", err, out)
			}
			cfg, _, err := project.LoadConfig(dir)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Gates.Gauntlet != "off" {
				t.Fatalf(".cernerc gauntlet = %q, want off", cfg.Gates.Gauntlet)
			}
			if !strings.Contains(out, invite) {
				t.Fatalf("init output lacks the invitation:\n%s", out)
			}
		})
	}
}
