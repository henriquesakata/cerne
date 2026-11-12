package project

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// isolateLookup keeps every F-0372 test off the real environment: no
// CERNE_ROOT from the session, no real cache/config/data dir.
func isolateLookup(t *testing.T) {
	t.Helper()
	t.Setenv("CERNE_ROOT", "")
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	t.Setenv("CERNE_DATA_DIR", t.TempDir())
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func mkProject(t *testing.T, dir, id string) {
	t.Helper()
	mkdirs(t, dir)
	if err := SaveConfig(dir, Config{ProjectID: id, Backend: "/tmp/b"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

// gitDir marks dir as a git repository root (.git directory).
func gitDir(t *testing.T, dir string) {
	t.Helper()
	mkdirs(t, filepath.Join(dir, ".git"))
}

// gitFile marks dir as a worktree/submodule root (.git file).
func gitFile(t *testing.T, dir string) {
	t.Helper()
	mkdirs(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /elsewhere/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// F-0372 crit. 1, 2: a nested git repository without .cernerc is never taken
// by the project above it.
func TestFindConfig_StopsAtNestedGitRoot(t *testing.T) {
	isolateLookup(t)
	for _, tc := range []struct {
		name       string
		projectGit bool
	}{{"project is a git repo", true}, {"project is not a git repo", false}} {
		t.Run(tc.name, func(t *testing.T) {
			proj := t.TempDir()
			mkProject(t, proj, "p")
			if tc.projectGit {
				gitDir(t, proj)
			}
			clone := filepath.Join(proj, "fixtures", "clone")
			gitDir(t, clone)
			start := filepath.Join(clone, "a", "b")
			mkdirs(t, start)

			_, dir, err := LoadConfig(start)
			if !errors.Is(err, ErrNoProject) {
				t.Fatalf("want ErrNoProject, got dir=%q err=%v", dir, err)
			}
			var crossed *crossedGitRootError
			if !errors.As(err, &crossed) || crossed.GitRoot != clone || crossed.ProjectDir != proj {
				t.Fatalf("want crossedGitRootError{%s, %s}, got %#v", clone, proj, err)
			}
			want := "no cerne project in this git repository (" + clone + "); the nearest .cernerc is at " + proj +
				", outside it — use --dir \"" + proj + "\" (or set CERNE_ROOT=\"" + proj + "\") to work on that project on purpose"
			if err.Error() != want {
				t.Fatalf("message:\n got %s\nwant %s", err, want)
			}
		})
	}
}

// F-0372 crit. 3: .cernerc at the git root, or between it and the cwd, resolves as before.
func TestFindConfig_SameRepoSubdir(t *testing.T) {
	isolateLookup(t)
	proj := t.TempDir()
	mkProject(t, proj, "p")
	gitDir(t, proj)
	start := filepath.Join(proj, "internal", "x")
	mkdirs(t, start)
	if _, dir, err := LoadConfig(start); err != nil || dir != proj {
		t.Fatalf("subdir of the project's repo: dir=%q err=%v", dir, err)
	}

	repo := t.TempDir()
	gitDir(t, repo)
	inner := filepath.Join(repo, "docs-project")
	mkProject(t, inner, "q")
	start = filepath.Join(inner, "y")
	mkdirs(t, start)
	if _, dir, err := LoadConfig(start); err != nil || dir != inner {
		t.Fatalf(".cernerc between git root and cwd: dir=%q err=%v", dir, err)
	}
}

// F-0372 crit. 4: outside any git repository the walk climbs as before.
func TestFindConfig_NoGitRepoWalksUp(t *testing.T) {
	isolateLookup(t)
	proj := t.TempDir()
	mkProject(t, proj, "p")
	start := filepath.Join(proj, "a", "b", "c")
	mkdirs(t, start)
	if _, dir, err := LoadConfig(start); err != nil || dir != proj {
		t.Fatalf("dir=%q err=%v", dir, err)
	}
}

// F-0372 crit. 5: a git worktree (.git as a file) is its own git root. This
// repository's layout — .claude/worktrees/<name>/ with a .git file and the
// versioned .cernerc at its root — still resolves to the worktree.
func TestFindConfig_WorktreeGitFile(t *testing.T) {
	isolateLookup(t)
	main := t.TempDir()
	mkProject(t, main, "p")
	gitDir(t, main)
	wt := filepath.Join(main, ".claude", "worktrees", "f0372")
	gitFile(t, wt)
	mkProject(t, wt, "p")
	start := filepath.Join(wt, "internal", "project")
	mkdirs(t, start)
	if _, dir, err := LoadConfig(start); err != nil || dir != wt {
		t.Fatalf("worktree with its .cernerc: dir=%q err=%v, want %q", dir, err, wt)
	}

	// A .git file without a .cernerc (a submodule) is a boundary like any other.
	sub := filepath.Join(main, "vendor", "lib")
	gitFile(t, sub)
	_, _, err := LoadConfig(sub)
	var crossed *crossedGitRootError
	if !errors.As(err, &crossed) || crossed.GitRoot != sub || crossed.ProjectDir != main {
		t.Fatalf("submodule: want crossedGitRootError{%s, %s}, got %v", sub, main, err)
	}
}

// F-0372 crit. 7: CERNE_ROOT, an explicit request, still wins at the boundary.
func TestFindConfig_CerneRootAfterGitBoundary(t *testing.T) {
	isolateLookup(t)
	proj := t.TempDir()
	mkProject(t, proj, "p")
	clone := filepath.Join(proj, "clone")
	gitDir(t, clone)

	t.Setenv("CERNE_ROOT", proj)
	if cfg, dir, err := LoadConfig(clone); err != nil || dir != proj || cfg.ProjectID != "p" {
		t.Fatalf("CERNE_ROOT at the boundary: id=%q dir=%q err=%v", cfg.ProjectID, dir, err)
	}

	t.Setenv("CERNE_ROOT", t.TempDir()) // no .cernerc there
	if _, _, err := LoadConfig(clone); !errors.Is(err, ErrNoProject) {
		t.Fatalf("bad CERNE_ROOT: want ErrNoProject, got %v", err)
	}
}
