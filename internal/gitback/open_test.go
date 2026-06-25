package gitback

import (
	"bytes"
	"os"
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
