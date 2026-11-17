package cli

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
)

// worktreeWithCerncerc returns a project whose .cernerc is committed and a
// linked worktree of it: .docs/ is gitignored, so the worktree has none.
// docsIgnore is the project's .gitignore rule for the projection; a test sets
// the anchored form, which does not cover a nested staging/.docs.
var docsIgnore = ".docs/\n"

func worktreeWithCerncerc(t *testing.T) (projDir, wt string) {
	t.Helper()
	return worktreeFixture(t, "")
}

// worktreeFixture is worktreeWithCerncerc with extra top-level .cernerc lines.
func worktreeFixture(t *testing.T, cernercPrefix string) (projDir, wt string) {
	t.Helper()
	t.Setenv("CERNE_ROOT", "")
	projDir, _, _ = setupProject(t)
	if cernercPrefix != "" {
		raw, err := os.ReadFile(filepath.Join(projDir, ".cernerc"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), append([]byte(cernercPrefix), raw...), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, projDir, "init", "-q")
	if err := os.WriteFile(filepath.Join(projDir, ".gitignore"), []byte(docsIgnore), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, projDir, "add", "-A")
	gitRun(t, projDir, "commit", "-q", "-m", "init")
	wt = filepath.Join(projDir, ".claude", "worktrees", "wt1")
	gitRun(t, projDir, "worktree", "add", "-q", "-b", "wt1", wt)
	if _, err := os.Stat(filepath.Join(wt, ".cernerc")); err != nil {
		t.Fatalf("worktree lacks .cernerc: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".docs")); err == nil {
		t.Fatal("worktree already has .docs/")
	}
	return projDir, wt
}

func sessionOut(t *testing.T, dir, format string) string {
	t.Helper()
	out, err := execCerneStdin(t, hookJSON(t, "s", dir, "", nil, nil), "hook", "session-context", "--format", format)
	if err != nil {
		t.Fatalf("%s: %v\n%s", format, err, out)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("%s: output is not valid JSON:\n%s", format, out)
	}
	return out
}

// B-0017: a worktree with its own .cernerc and no .docs/ gets the projection
// materialized by SessionStart from the local clone, in every format.
func TestSessionContextMaterializesMissingDocs(t *testing.T) {
	for _, format := range []string{"claude", "codex", "cursor", "gemini", "copilot"} {
		t.Run(format, func(t *testing.T) {
			_, wt := worktreeWithCerncerc(t)
			out := sessionOut(t, wt, format)
			if _, err := os.Stat(filepath.Join(wt, ".docs", "CONTEXT.md")); err != nil {
				t.Fatalf("CONTEXT.md not materialized: %v\n%s", err, out)
			}
			if !strings.Contains(out, "just materialized from the local cache") || strings.Contains(out, "Materialized:") {
				t.Fatalf("want the stale-cache notice and no pull output:\n%s", out)
			}
			if m, _ := filepath.Glob(filepath.Join(wt, ".docs-staging-*")); len(m) > 0 {
				t.Fatalf("staging dir left behind: %v", m)
			}
		})
	}
}

// repoOf returns the cache clone of the project at projDir.
func repoOf(t *testing.T, projDir string) string {
	t.Helper()
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatal(err)
	}
	cacheDir, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(cacheDir, "repo")
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// B-0017 (critic blocker): SessionStart never fetches or resets: a clone with
// an unpushed local commit keeps it, and .docs/ is built from that clone.
func TestSessionContextKeepsUnpushedCloneState(t *testing.T) {
	projDir, wt := worktreeWithCerncerc(t)
	repo := repoOf(t, projDir)
	if err := os.WriteFile(filepath.Join(repo, "unsent.txt"), []byte("u"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "unsent.txt")
	gitRun(t, repo, "commit", "-q", "-m", "unpushed")
	head := gitOut(t, repo, "rev-parse", "HEAD")
	if gitOut(t, repo, "rev-list", "--count", "HEAD", "--not", "--remotes") != "1" {
		t.Fatal("fixture: clone is not one commit ahead of origin")
	}

	sessionOut(t, wt, "claude")
	if got := gitOut(t, repo, "rev-parse", "HEAD"); got != head {
		t.Fatalf("clone HEAD moved: %s -> %s", head, got)
	}
	if _, err := os.Stat(filepath.Join(repo, "unsent.txt")); err != nil {
		t.Fatalf("unpushed file lost: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".docs", "CONTEXT.md")); err != nil {
		t.Fatalf("CONTEXT.md missing: %v", err)
	}
}

// B-0017: nothing of the materialization reaches the real stdout/stderr (it
// does not go through cobra's writers), and the envelope stays valid JSON.
func TestSessionContextRealFdsStayClean(t *testing.T) {
	_, wt := worktreeWithCerncerc(t)
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	restore := func() { os.Stdout, os.Stderr = oldOut, oldErr }
	defer restore()
	os.Stdout, os.Stderr = wOut, wErr
	out := sessionOut(t, wt, "cursor")
	restore()
	wOut.Close()
	wErr.Close()
	gotOut, _ := io.ReadAll(rOut)
	gotErr, _ := io.ReadAll(rErr)
	if len(gotOut) != 0 || len(gotErr) != 0 {
		t.Fatalf("leaked to real fds: stdout=%q stderr=%q", gotOut, gotErr)
	}
	if !strings.Contains(out, "additional_context") {
		t.Fatalf("envelope: %s", out)
	}
}

// B-0017: no clone (first use on this machine) or a lock that stays held: one
// notice, exit 0, quickly, nothing materialized. A holder that finishes the
// projection meanwhile means no notice at all.
func TestSessionContextNoticeWhenNotMaterializable(t *testing.T) {
	projDir, wt := worktreeWithCerncerc(t)
	repo := repoOf(t, projDir)
	old := sessionLockWait
	t.Cleanup(func() { sessionLockWait = old })
	sessionLockWait = 50 * time.Millisecond

	// lock held past the wait
	err := gitback.WithLock(gitback.LockPath(repo), func() error {
		start := time.Now()
		out := sessionOut(t, wt, "claude")
		if strings.Count(out, "run `cerne pull`") != 1 || time.Since(start) > 5*time.Second {
			t.Errorf("held lock: want one notice, quickly:\n%s", out)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".docs")); err == nil {
		t.Fatal(".docs/ created while the lock was held")
	}

	// holder wrote the projection meanwhile: re-check, no notice
	_ = gitback.WithLock(gitback.LockPath(repo), func() error {
		mkdirs := os.MkdirAll(filepath.Join(wt, ".docs"), 0o755)
		if mkdirs != nil {
			t.Fatal(mkdirs)
		}
		if err := os.WriteFile(filepath.Join(wt, ".docs", "CONTEXT.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if out := sessionOut(t, wt, "claude"); strings.Contains(out, "cerne pull") {
			t.Errorf("notice although CONTEXT.md appeared:\n%s", out)
		}
		return nil
	})

	// no clone
	if err := os.RemoveAll(filepath.Join(wt, ".docs")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	if out := sessionOut(t, wt, "gemini"); strings.Count(out, "run `cerne pull`") != 1 {
		t.Fatalf("no clone: want one notice:\n%s", out)
	}
}

// B-0017: with .docs/ present nothing changes and nothing is built; a worktree
// without its own .cernerc resolves to the main checkout (B-0016), whose
// .docs/ exists.
func TestSessionContextNoPullWhenDocsPresent(t *testing.T) {
	projDir, wt := worktreeWithCerncerc(t)
	docs := filepath.Join(projDir, ".docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "CONTEXT.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := sessionOut(t, projDir, "claude"); strings.Contains(out, "cerne pull") {
		t.Fatalf("notice with .docs/ present:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(docs, "CONTEXT.md")); string(b) != "x" {
		t.Fatal(".docs/CONTEXT.md was rewritten")
	}

	if err := os.Remove(filepath.Join(wt, ".cernerc")); err != nil {
		t.Fatal(err)
	}
	if out := sessionOut(t, wt, "claude"); strings.Contains(out, "cerne pull") {
		t.Fatalf("notice for a worktree resolving to the main checkout:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(wt, ".docs")); err == nil {
		t.Fatal(".docs/ created in a worktree that uses the main project")
	}
}

// withProjection gives projDir a .docs/CONTEXT.md, for tests that compare the
// board output across calls: the first SessionStart would otherwise build the
// projection and add its stale-cache notice (B-0017).
func withProjection(t *testing.T, projDir string) {
	t.Helper()
	docs := filepath.Join(projDir, ".docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "CONTEXT.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func porcelain(t *testing.T, dir string) string { return gitOut(t, dir, "status", "--porcelain") }

// B-0017 (critic 2, blocker): the staging directory is invisible to git while
// it exists — also when a killed run left one behind — and after the build.
func TestSessionContextStagingInvisibleToGit(t *testing.T) {
	oldIgnore := docsIgnore
	t.Cleanup(func() { docsIgnore = oldIgnore })
	docsIgnore = "/.docs/\n" // does not cover <stage>/.docs
	_, wt := worktreeWithCerncerc(t)
	stale := filepath.Join(wt, ".docs-staging-dead")
	if err := os.MkdirAll(filepath.Join(stale, ".docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, ".gitignore"), []byte("*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, ".docs", "CONTEXT.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := porcelain(t, wt); got != "" {
		t.Fatalf("a leftover staging dir is visible to git:\n%s", got)
	}

	var midBuild string
	hookRan := false
	old := materializeHook
	t.Cleanup(func() { materializeHook = old })
	materializeHook = func(repoDir string) { hookRan = true; midBuild = porcelain(t, wt) }
	sessionOut(t, wt, "claude")
	if !hookRan {
		t.Fatal("the hook did not build")
	}
	if midBuild != "" {
		t.Fatalf("the staging dir is visible to git mid-build:\n%s", midBuild)
	}
	if got := porcelain(t, wt); got != "" {
		t.Fatalf("git sees changes after the hook:\n%s", got)
	}
	if m, _ := filepath.Glob(filepath.Join(wt, ".docs-staging-*")); len(m) > 0 {
		t.Fatalf("stale staging not swept: %v", m)
	}
}

// B-0017 (critic 2): a .docs/ that exists without CONTEXT.md is never wiped
// (it may hold unpushed edits or .conflicts/): notice, nothing touched.
func TestSessionContextKeepsExistingDocsDir(t *testing.T) {
	_, wt := worktreeWithCerncerc(t)
	edit := filepath.Join(wt, ".docs", "current", "features", "F-0001-x", "spec.md")
	if err := os.MkdirAll(filepath.Dir(edit), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(edit, []byte("unpushed"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := sessionOut(t, wt, "claude")
	if b, err := os.ReadFile(edit); err != nil || string(b) != "unpushed" {
		t.Fatalf("unpushed edit lost: %v %q", err, b)
	}
	if strings.Count(out, "run `cerne pull`") != 1 {
		t.Fatalf("want the cerne pull notice:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(wt, ".docs", "CONTEXT.md")); err == nil {
		t.Fatal("CONTEXT.md written into an existing .docs/")
	}
}

// B-0017 (critic 3): the clone moving under the build (a writer that did not
// take the lock) discards the build and emits the notice.
func TestSessionContextDiscardsBuildWhenCloneMoves(t *testing.T) {
	projDir, wt := worktreeWithCerncerc(t)
	repo := repoOf(t, projDir)
	old := materializeHook
	t.Cleanup(func() { materializeHook = old })
	materializeHook = func(repoDir string) {
		if err := os.WriteFile(filepath.Join(repoDir, "moved.txt"), []byte("m"), 0o644); err != nil {
			t.Error(err)
		}
		gitRun(t, repoDir, "add", "moved.txt")
		gitRun(t, repoDir, "commit", "-q", "-m", "concurrent writer")
	}
	out := sessionOut(t, wt, "claude")
	_ = repo
	if strings.Count(out, "run `cerne pull`") != 1 {
		t.Fatalf("want the cerne pull notice:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(wt, ".docs")); err == nil {
		t.Fatal(".docs/ installed although the clone moved mid-build")
	}
	if m, _ := filepath.Glob(filepath.Join(wt, ".docs-staging-*")); len(m) > 0 {
		t.Fatalf("staging left behind: %v", m)
	}
}

// B-0017 (critic 4): the sidecars land in .docs/ where pull writes them, so a
// following push has nothing to send and a pull neither conflicts nor drifts —
// with pin_branch set, on a branch that is not the pin branch.
func TestSessionContextSidecarsMatchPull(t *testing.T) {
	_, wt := worktreeFixture(t, "pin_branch = \"main\"\n")
	lockBefore, _ := os.ReadFile(filepath.Join(wt, "docs.lock"))
	sessionOut(t, wt, "claude")
	for _, f := range []string{".manifest.json", ".snapshot"} {
		if _, err := os.Stat(filepath.Join(wt, ".docs", f)); err != nil {
			t.Fatalf("%s missing: %v", f, err)
		}
	}
	out, err := execCerne(t, "push", "--dry-run", "--dir", wt)
	if err != nil || !strings.Contains(out, "Nothing to send") {
		t.Fatalf("push --dry-run after the hook: %v\n%s", err, out)
	}
	out, err = execCerne(t, "pull", "--dir", wt)
	if err != nil || strings.Contains(strings.ToLower(out), "conflict") || strings.Contains(strings.ToLower(out), "local edit") {
		t.Fatalf("pull after the hook: %v\n%s", err, out)
	}
	if lockAfter, _ := os.ReadFile(filepath.Join(wt, "docs.lock")); string(lockAfter) != string(lockBefore) {
		t.Fatalf("docs.lock moved off the pin branch:\n%s\n->\n%s", lockBefore, lockAfter)
	}
}
