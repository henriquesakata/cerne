package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
)

// setupGitProject is setupProject with the project dir initialized as a git
// repo first, so the docs.lock merge driver has somewhere to register.
func setupGitProject(t *testing.T) (projDir, origin, repoDir string) {
	t.Helper()
	cacheRoot := t.TempDir()
	t.Setenv("CERNE_CACHE_DIR", cacheRoot)
	origin = newOrigin(t)
	projDir = t.TempDir()
	if _, err := git.PlainInit(projDir, false); err != nil {
		t.Fatalf("git init projDir: %v", err)
	}
	if out, err := execInit(t, "--name", "demo", "--backend", origin, "--dir", projDir); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	return projDir, origin, filepath.Join(cacheRoot, "demo", "repo")
}

func assertMergeDriverRegistered(t *testing.T, projDir string) {
	t.Helper()
	attrs, err := os.ReadFile(filepath.Join(projDir, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	if !strings.Contains(string(attrs), "docs.lock merge=cerne-lock") {
		t.Fatalf(".gitattributes = %q, want the docs.lock merge line", attrs)
	}
	repo, err := git.PlainOpen(projDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if got := cfg.Raw.Section("merge").Subsection("cerne-lock").Option("driver"); got != "cerne lock merge %O %A %B" {
		t.Fatalf("merge.cerne-lock.driver = %q, want the driver command", got)
	}
}

func TestInitRegistersMergeDriver(t *testing.T) {
	projDir, _, _ := setupGitProject(t)
	assertMergeDriverRegistered(t, projDir)
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// A git worktree keeps .git as a file pointing at a shared gitdir; the driver
// must still register in the shared config (this is the parallel-worktree
// workflow case 0030 targets). Regression guard for EnableDotGitCommonDir.
func TestPullSelfHealsMergeDriverInWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	if runtime.GOOS == "windows" {
		// go-git keeps a handle on the worktree's gitdir open within this
		// in-process test, so t.TempDir cleanup can't unlink it on Windows. The
		// behaviour itself works there (validated manually); CI (Linux) runs this
		// guard. Real usage runs 'cerne pull' as a separate, exiting process.
		t.Skip("worktree gitdir handle blocks t.TempDir cleanup on Windows; covered on CI (Linux)")
	}
	projDir, _, _ := setupGitProject(t)
	runGit(t, projDir, "config", "user.email", "t@t")
	runGit(t, projDir, "config", "user.name", "t")
	runGit(t, projDir, "add", ".cernerc", "docs.lock", ".gitattributes")
	runGit(t, projDir, "commit", "-m", "base")
	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, projDir, "worktree", "add", "-b", "wtbranch", wt, "HEAD")
	runGit(t, projDir, "config", "--remove-section", "merge.cerne-lock")

	if out, err := execCerne(t, "pull", "--dir", wt); err != nil {
		t.Fatalf("pull in worktree: %v\n%s", err, out)
	}
	got := strings.TrimSpace(runGit(t, wt, "config", "--get", "merge.cerne-lock.driver"))
	if got != "cerne lock merge %O %A %B" {
		t.Fatalf("driver from worktree = %q, want it registered in the shared config", got)
	}
}

// extensions.worktreeConfig is what git enables on the shared config of a repo
// using per-worktree config (the default for this project's parallel worktrees).
// go-git validates extensions on open and refuses that one, so it can't open the
// repo for a config write at all — the open fails before SetConfig. The driver
// must still register via the git-binary fallback. This is the faithful
// end-to-end scenario; TestPullRegistersMergeDriverWithWorktreeConfigExtension
// is the all-platforms guard for the same go-git open failure.
func TestPullRegistersMergeDriverInWorktreeWithWorktreeConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	if runtime.GOOS == "windows" {
		// Same limitation as TestPullSelfHealsMergeDriverInWorktree: go-git keeps a
		// handle on the worktree's gitdir within this in-process test, so t.TempDir
		// cleanup can't unlink it on Windows. The fallback itself works there
		// (validated manually + by the non-worktree guard below); CI (Linux) runs
		// this one. Real usage runs 'cerne pull' as a separate, exiting process.
		t.Skip("worktree gitdir handle blocks t.TempDir cleanup on Windows; covered on CI (Linux)")
	}
	projDir, _, _ := setupGitProject(t)
	runGit(t, projDir, "config", "user.email", "t@t")
	runGit(t, projDir, "config", "user.name", "t")
	runGit(t, projDir, "config", "extensions.worktreeConfig", "true")
	runGit(t, projDir, "add", ".cernerc", "docs.lock", ".gitattributes")
	runGit(t, projDir, "commit", "-m", "base")
	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, projDir, "worktree", "add", "-b", "wtbranch", wt, "HEAD")
	// Fresh clone: the local merge.* config never travels, so it starts absent.
	runGit(t, projDir, "config", "--remove-section", "merge.cerne-lock")

	if out, err := execCerne(t, "pull", "--dir", wt); err != nil {
		t.Fatalf("pull in worktree: %v\n%s", err, out)
	}
	got := strings.TrimSpace(runGit(t, wt, "config", "--get", "merge.cerne-lock.driver"))
	if got != "cerne lock merge %O %A %B" {
		t.Fatalf("driver from worktree = %q, want it registered via the git-binary fallback", got)
	}
}

// extensions.worktreeConfig on the repo config is the exact shared-config state a
// worktree presents to go-git, and the precise trigger for the open failure —
// without needing an actual worktree gitdir (which leaks a go-git handle that
// blocks t.TempDir cleanup on Windows). So this guards the git-binary fallback on
// every platform, including the Windows dev box where the bug surfaced.
func TestPullRegistersMergeDriverWithWorktreeConfigExtension(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	projDir, _, _ := setupGitProject(t)
	runGit(t, projDir, "config", "extensions.worktreeConfig", "true")
	// Fresh clone: the local merge.* config never travels, so it starts absent.
	runGit(t, projDir, "config", "--remove-section", "merge.cerne-lock")

	// go-git can no longer open this repo for a config write — assert the trigger.
	if _, err := git.PlainOpenWithOptions(projDir, &git.PlainOpenOptions{EnableDotGitCommonDir: true}); err == nil {
		t.Fatal("go-git opened a repo declaring extensions.worktreeConfig; the fallback would be untested")
	}

	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	got := strings.TrimSpace(runGit(t, projDir, "config", "--get", "merge.cerne-lock.driver"))
	if got != "cerne lock merge %O %A %B" {
		t.Fatalf("driver = %q, want it registered via the git-binary fallback", got)
	}
	attrs, err := os.ReadFile(filepath.Join(projDir, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	if !strings.Contains(string(attrs), "docs.lock merge=cerne-lock") {
		t.Fatalf(".gitattributes = %q, want the docs.lock merge line", attrs)
	}
}

func TestPullSelfHealsMergeDriver(t *testing.T) {
	projDir, _, _ := setupGitProject(t)
	// Simulate a fresh clone: .gitattributes travels (committed) but the local
	// .git/config never does, so the driver is unregistered until something
	// self-heals it.
	repo, err := git.PlainOpen(projDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	cfg, err := repo.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	cfg.Raw.RemoveSubsection("merge", "cerne-lock")
	if err := repo.SetConfig(cfg); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	if out, err := execCerne(t, "pull", "--dir", projDir); err != nil {
		t.Fatalf("pull: %v\n%s", err, out)
	}
	assertMergeDriverRegistered(t, projDir)
}

func writeTempLock(t *testing.T, l project.Lock) string {
	t.Helper()
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	p := filepath.Join(t.TempDir(), "docs.lock")
	if err := os.WriteFile(p, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write temp lock: %v", err)
	}
	return p
}

func readLockAt(t *testing.T, path string) project.Lock {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lock %s: %v", path, err)
	}
	var l project.Lock
	if err := json.Unmarshal(data, &l); err != nil {
		t.Fatalf("unmarshal lock %s: %v", path, err)
	}
	return l
}

// commitSiblingOf creates, in repoDir, a commit whose parent is `parent`
// (on a detached HEAD): a sibling of parent's other child, so neither is an
// ancestor of the other.
func commitSiblingOf(t *testing.T, repoDir, parent, filename string) string {
	t.Helper()
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	if err := wt.Checkout(&git.CheckoutOptions{Hash: plumbing.NewHash(parent), Force: true}); err != nil {
		t.Fatalf("Checkout %s: %v", parent, err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, filename), []byte("sibling"), 0o644); err != nil {
		t.Fatalf("write sibling: %v", err)
	}
	if _, err := wt.Add(filename); err != nil {
		t.Fatalf("Add: %v", err)
	}
	sig := &object.Signature{Name: "t", Email: "t@t", When: time.Now()}
	h, err := wt.Commit("sibling", &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		t.Fatalf("Commit sibling: %v", err)
	}
	return h.String()
}

func TestLockMergeChoosesDescendant(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	a, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA A: %v", err)
	}
	if out, err := execCerne(t, "feature", "new", "Descendant", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	b, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA B: %v", err)
	}
	if a == b {
		t.Fatalf("backend did not advance")
	}

	// ours at the ancestor, theirs at the descendant → resolve to theirs.
	base := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: a})
	ours := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: a})
	theirs := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: b})
	if out, err := execCerne(t, "lock", "merge", "--dir", projDir, base, ours, theirs); err != nil {
		t.Fatalf("lock merge: %v\n%s", err, out)
	}
	if got := readLockAt(t, ours).Snapshot; got != b {
		t.Fatalf("resolved snapshot = %s, want descendant %s", got, b)
	}

	// Symmetric: ours at the descendant, theirs at the ancestor → keep ours.
	ours2 := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: b})
	theirs2 := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: a})
	if out, err := execCerne(t, "lock", "merge", "--dir", projDir, base, ours2, theirs2); err != nil {
		t.Fatalf("lock merge symmetric: %v\n%s", err, out)
	}
	if got := readLockAt(t, ours2).Snapshot; got != b {
		t.Fatalf("symmetric resolved snapshot = %s, want descendant %s", got, b)
	}
}

func TestLockMergeBackendMismatch(t *testing.T) {
	projDir, origin, _ := setupProject(t)
	base := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: "a"})
	ours := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: "a"})
	theirs := writeTempLock(t, project.Lock{Version: 1, Backend: "git@other:repo.git", Snapshot: "b"})
	out, err := execCerne(t, "lock", "merge", "--dir", projDir, base, ours, theirs)
	if err == nil {
		t.Fatalf("lock merge succeeded on backend mismatch, want error\n%s", out)
	}
	if got := readLockAt(t, ours).Snapshot; got != "a" {
		t.Fatalf("ours snapshot = %s, want untouched 'a' (driver must not overwrite on a real conflict)", got)
	}
}

func TestLockMergeDivergent(t *testing.T) {
	projDir, origin, repoDir := setupProject(t)
	a, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA A: %v", err)
	}
	if out, err := execCerne(t, "feature", "new", "Branch B", "--context", "ctx de teste", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	b, err := gitback.HeadSHA(repoDir)
	if err != nil {
		t.Fatalf("HeadSHA B: %v", err)
	}
	c := commitSiblingOf(t, repoDir, a, "sibling.txt") // sibling of b: neither is ancestor

	base := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: a})
	ours := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: b})
	theirs := writeTempLock(t, project.Lock{Version: 1, Backend: origin, Snapshot: c})
	out, err := execCerne(t, "lock", "merge", "--dir", projDir, base, ours, theirs)
	if err == nil {
		t.Fatalf("lock merge succeeded on divergent snapshots, want error\n%s", out)
	}
	if got := readLockAt(t, ours).Snapshot; got != b {
		t.Fatalf("ours snapshot = %s, want untouched %s", got, b)
	}
}
