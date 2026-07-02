package gitback

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// worktreeConfigRepo creates a repo with one commit and then declares
// extensions.worktreeConfig=true on its config — the exact shared-config state a
// parallel git worktree presents to go-git, and the precise trigger for the
// F-0075 open failure. The extension is added last, via go-git, because go-git
// can no longer open the repo once it is present (which is the whole point).
func worktreeConfigRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("a.txt"); err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "t", Email: "t@local", When: time.Now()}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatalf("commit: %v", err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Raw.Section("extensions").SetOption("worktreeConfig", "true")
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatalf("set extension: %v", err)
	}
	return dir
}

func TestOpenRepoToleratesWorktreeConfigExtension(t *testing.T) {
	dir := worktreeConfigRepo(t)

	// Precondition: go-git itself refuses the repo. Documents the showstopper
	// F-0075 fixes — if go-git ever starts tolerating it, this guard goes stale.
	if _, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{EnableDotGitCommonDir: true}); !IsUnsupportedExtension(err) {
		t.Fatalf("precondition: want unsupported-extension error, got %v", err)
	}

	repo, err := openRepo(dir)
	if err != nil {
		t.Fatalf("openRepo tolerated nothing: %v", err)
	}
	if _, err := repo.Head(); err != nil {
		t.Fatalf("Head through tolerant open: %v", err)
	}

	// Invariant (crit. 4): cerne does not mutate the user's git config to work
	// around the extension — it must still be there afterwards.
	cfg, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(cfg, []byte("worktreeConfig")) {
		t.Errorf("extension removed from .git/config; cerne must not rewrite it.\n%s", cfg)
	}
}

func TestGitbackOpsTolerateWorktreeConfigExtension(t *testing.T) {
	dir := worktreeConfigRepo(t)

	if _, err := HeadSHA(dir); err != nil {
		t.Fatalf("HeadSHA under worktreeConfig: %v", err)
	}

	// CommitPath is the exact F-0075 showstopper: `cerne feature new` with
	// autocommit_lock pins docs.lock via CommitPath on the project repo, which a
	// worktreeConfig declaration would otherwise make impossible.
	if err := os.WriteFile(filepath.Join(dir, "docs.lock"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitPath(dir, "docs.lock", "pin docs.lock"); err != nil {
		t.Fatalf("CommitPath under worktreeConfig: %v", err)
	}
}

// linkedWorktree creates a repo with one commit plus a linked worktree via the
// real git binary (go-git cannot create worktrees), skipping when git is
// unavailable. Returns the main checkout and the worktree paths.
func linkedWorktree(t *testing.T) (mainDir, wtDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary unavailable")
	}
	mainDir = t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main", mainDir},
		{"-C", mainDir, "config", "user.email", "t@local"},
		{"-C", mainDir, "config", "user.name", "t"},
		{"-C", mainDir, "commit", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	wtDir = t.TempDir() + "/wt"
	if out, err := exec.Command("git", "-C", mainDir, "worktree", "add", "-b", "wt-branch", wtDir).CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, out)
	}
	return mainDir, wtDir
}

// openRepo on a linked worktree must not keep a handle on the worktree's admin
// dir: go-git's PlainOpen leaks the commondir file it reads during resolution
// (dotGitCommonDirectory never closes it, go-git v5.19.1), and on Windows that
// leaked handle makes .git/worktrees/<name> undeletable — by git itself, by
// the user, by t.TempDir cleanup — until the process exits or the GC happens
// to finalize the orphaned file.
func TestOpenRepoLinkedWorktreeReleasesAdminDir(t *testing.T) {
	mainDir, wtDir := linkedWorktree(t)
	repo, err := openRepo(wtDir)
	if err != nil {
		t.Fatalf("openRepo(worktree): %v", err)
	}
	if _, err := repo.Head(); err != nil {
		t.Fatalf("Head(worktree): %v", err)
	}
	if err := os.RemoveAll(filepath.Join(mainDir, ".git", "worktrees")); err != nil {
		t.Fatalf("admin dir still held after openRepo (leaked commondir handle): %v", err)
	}
}

// TestNoRawPlainOpen is the crit. 5 guard: every git open in this package must
// route through openRepo, so a new open site can't silently reintroduce the
// F-0075 showstopper. open.go is the one sanctioned caller (PlainOpenWithOptions
// is its fast path); test files are exempt (they assert on the raw behavior).
func TestNoRawPlainOpen(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range matches {
		if f == "open.go" || strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(src, []byte("git.PlainOpen")) {
			t.Errorf("%s calls git.PlainOpen directly; use openRepo so extensions.worktreeConfig stays tolerated (F-0075)", f)
		}
	}
}
