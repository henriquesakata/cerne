package gitback

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The native-git fast path is only safe if it is indistinguishable from the
// go-git one. These tests run the same scenario through both backends and
// compare the observable result — swapping gitBin is the switch.

func withoutGitBin(t *testing.T) {
	t.Helper()
	prev := gitBin
	gitBin = func() string { return "" }
	t.Cleanup(func() { gitBin = prev })
}

func requireGitBin(t *testing.T) {
	t.Helper()
	if gitBin() == "" {
		t.Skip("git binary unavailable")
	}
}

// dirtyRepo seeds a repo and dirties it the three ways the reconcile loop has
// to capture: a modification, an addition (untracked) and a deletion.
func dirtyRepo(t *testing.T) string {
	t.Helper()
	dir := initRepoWith(t, map[string]string{"keep.md": "keep\n", "edit.md": "before\n", "gone.md": "gone\n"})
	writeFile(t, dir, "edit.md", "after\n")
	writeFile(t, dir, "added.md", "new\n")
	if err := os.Remove(filepath.Join(dir, "gone.md")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	return dir
}

// hostileRepo is dirtyRepo's nastier sibling: the shapes that actually break a
// porcelain parser — a name with an accent (git quotes it octally unless -z), a
// name with a space, an untracked file nested in a NEW directory (plain
// --porcelain collapses that to "dir/" and lists nothing inside), and an
// ignored file (neither backend may ever report it).
func hostileRepo(t *testing.T) string {
	t.Helper()
	dir := initRepoWith(t, map[string]string{
		".gitignore":      "ignored/\n*.tmp\n",
		"ação usuário.md": "antes\n",
	})
	writeFile(t, dir, "ação usuário.md", "depois\n")
	for _, rel := range []string{"nova pasta/aninhado.md", "ignored/nope.md"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeFile(t, dir, rel, "x\n")
	}
	writeFile(t, dir, "scratch.tmp", "x\n")
	return dir
}

func TestWorktreeChangesHostileNamesBackendsAgree(t *testing.T) {
	requireGitBin(t)
	exec, err := WorktreeChanges(hostileRepo(t))
	if err != nil {
		t.Fatalf("exec WorktreeChanges: %v", err)
	}
	dir := hostileRepo(t)
	withoutGitBin(t)
	gogit, err := WorktreeChanges(dir)
	if err != nil {
		t.Fatalf("go-git WorktreeChanges: %v", err)
	}
	want := []Change{{Path: "ação usuário.md"}, {Path: "nova pasta/aninhado.md"}}
	if !reflect.DeepEqual(gogit, want) {
		t.Fatalf("go-git changes = %+v, want %+v", gogit, want)
	}
	if !reflect.DeepEqual(exec, gogit) {
		t.Fatalf("exec changes = %+v, want %+v", exec, gogit)
	}
}

func TestCommitPathsHostileNamesBackendsAgree(t *testing.T) {
	requireGitBin(t)
	paths := []string{"ação usuário.md", "nova pasta/aninhado.md"}
	commit := func(dir string) string {
		t.Helper()
		if _, err := CommitPaths(dir, paths, "probe"); err != nil {
			t.Fatalf("CommitPaths: %v", err)
		}
		blob, ok, err := FileBlobAt(dir, headOf(t, dir), "nova pasta/aninhado.md")
		if err != nil || !ok {
			t.Fatalf("nested path not committed: ok=%v err=%v", ok, err)
		}
		left, err := WorktreeChanges(dir)
		if err != nil {
			t.Fatalf("WorktreeChanges: %v", err)
		}
		if len(left) != 0 {
			t.Fatalf("worktree not clean after commit: %+v", left)
		}
		return string(blob)
	}
	execBlob := commit(hostileRepo(t))
	dir := hostileRepo(t)
	withoutGitBin(t)
	if gogitBlob := commit(dir); gogitBlob != execBlob {
		t.Fatalf("committed blob: exec=%q go-git=%q", execBlob, gogitBlob)
	}
}

func TestWorktreeChangesBackendsAgree(t *testing.T) {
	requireGitBin(t)
	exec, err := WorktreeChanges(dirtyRepo(t))
	if err != nil {
		t.Fatalf("exec WorktreeChanges: %v", err)
	}
	dir := dirtyRepo(t)
	withoutGitBin(t)
	gogit, err := WorktreeChanges(dir)
	if err != nil {
		t.Fatalf("go-git WorktreeChanges: %v", err)
	}
	want := []Change{
		{Path: "added.md"}, {Path: "edit.md"}, {Path: "gone.md", Deleted: true},
	}
	if !reflect.DeepEqual(gogit, want) {
		t.Fatalf("go-git changes = %+v, want %+v", gogit, want)
	}
	if !reflect.DeepEqual(exec, gogit) {
		t.Fatalf("exec changes = %+v, want %+v", exec, gogit)
	}
}

func TestCommitPathsBackendsAgree(t *testing.T) {
	requireGitBin(t)
	paths := []string{"edit.md", "added.md", "gone.md"}

	commitAndRead := func(dir string) (string, []Change) {
		t.Helper()
		if _, err := CommitPaths(dir, paths, "probe"); err != nil {
			t.Fatalf("CommitPaths: %v", err)
		}
		// The committed tree is what matters, not the sha (timestamps differ).
		blob, ok, err := FileBlobAt(dir, headOf(t, dir), "edit.md")
		if err != nil || !ok {
			t.Fatalf("FileBlobAt edit.md: ok=%v err=%v", ok, err)
		}
		if _, ok, _ := FileBlobAt(dir, headOf(t, dir), "gone.md"); ok {
			t.Fatalf("gone.md still in the committed tree")
		}
		left, err := WorktreeChanges(dir)
		if err != nil {
			t.Fatalf("WorktreeChanges: %v", err)
		}
		return string(blob), left
	}

	execBlob, execLeft := commitAndRead(dirtyRepo(t))
	dir := dirtyRepo(t)
	withoutGitBin(t)
	gogitBlob, gogitLeft := commitAndRead(dir)

	if execBlob != "after\n" || gogitBlob != "after\n" {
		t.Fatalf("committed edit.md: exec=%q go-git=%q, want %q", execBlob, gogitBlob, "after\n")
	}
	if len(execLeft) != 0 || len(gogitLeft) != 0 {
		t.Fatalf("worktree not clean after commit: exec=%+v go-git=%+v", execLeft, gogitLeft)
	}
}

func TestCommitPathsNothingToCommitBackendsAgree(t *testing.T) {
	requireGitBin(t)
	_, err := CommitPaths(initRepoWith(t, map[string]string{"a.md": "a\n"}), []string{"a.md"}, "probe")
	if !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("exec clean commit = %v, want ErrNothingToCommit", err)
	}
	dir := initRepoWith(t, map[string]string{"a.md": "a\n"})
	withoutGitBin(t)
	if _, err := CommitPaths(dir, []string{"a.md"}, "probe"); !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("go-git clean commit = %v, want ErrNothingToCommit", err)
	}
}

func TestResetHardBackendsAgree(t *testing.T) {
	requireGitBin(t)
	// Both backends must land on a worktree that matches the tree exactly:
	// the modification reverted, the deletion restored, the untracked file gone
	// (the reconcile loop relies on the reset leaving nothing behind).
	assertClean := func(dir string) {
		t.Helper()
		if err := ResetHard(dir, headOf(t, dir)); err != nil {
			t.Fatalf("ResetHard: %v", err)
		}
		left, err := WorktreeChanges(dir)
		if err != nil {
			t.Fatalf("WorktreeChanges: %v", err)
		}
		if len(left) != 0 {
			t.Fatalf("worktree still dirty after reset: %+v", left)
		}
		if _, err := os.Stat(filepath.Join(dir, "added.md")); !os.IsNotExist(err) {
			t.Fatalf("untracked file survived the reset: %v", err)
		}
	}
	assertClean(dirtyRepo(t))
	dir := dirtyRepo(t)
	withoutGitBin(t)
	assertClean(dir)
}

// The darwin stub rewrite must never fire anywhere else: on Linux the real git
// IS /usr/bin/git, and treating it as a stub silently disables the whole fast
// path on every Linux host, CI included — with every other test still green,
// because they would all just run go-git twice.
func TestGitBinNotDisabledWhereGitIsReal(t *testing.T) {
	if os.Getenv("CERNE_NO_GIT_BIN") != "" {
		t.Skip("fast path deliberately disabled for this run")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on PATH")
	}
	if runtime.GOOS == "darwin" {
		// Only here may LookPath find a binary that is not usable (the stub of
		// a Mac with no Command Line Tools), so "" is a legitimate answer.
		t.Skip("darwin: /usr/bin/git may be a stub with nothing behind it")
	}
	if gitBin() == "" {
		t.Fatal("git is on PATH but gitBin() is empty — the fast path is off and nothing else would notice")
	}
	if got := unshimmed("/usr/bin/git"); got != "/usr/bin/git" {
		t.Fatalf("unshimmed(/usr/bin/git) = %q, want it untouched off darwin", got)
	}
}

func TestCommitPathsEmptyPathsCommitsNothing(t *testing.T) {
	requireGitBin(t)
	// `git add --all --` with no pathspec stages the entire tree; the guard
	// must refuse before that, leaving the worktree exactly as it was.
	dir := dirtyRepo(t)
	before, err := WorktreeChanges(dir)
	if err != nil {
		t.Fatalf("WorktreeChanges: %v", err)
	}
	if _, err := CommitPaths(dir, nil, "probe"); !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("CommitPaths(nil) = %v, want ErrNothingToCommit", err)
	}
	after, err := WorktreeChanges(dir)
	if err != nil {
		t.Fatalf("WorktreeChanges: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("worktree changed: before=%+v after=%+v", before, after)
	}
}

// A grandchild holding the inherited pipes past WaitDelay makes Run() report
// ErrWaitDelay even when the command itself exited 0 — git's credential-cache
// daemon and the ControlPersist master this package starts both do exactly
// that. Reading it as failure would turn a push that landed into a retry.
func TestFinishGitCmdSucceedsDespiteWaitDelay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /bin/sh")
	}
	cmd := exec.Command("/bin/sh", "-c", "sleep 3 & exit 0")
	cmd.Stdout, cmd.Stderr = &strings.Builder{}, &strings.Builder{}
	cmd.WaitDelay = 100 * time.Millisecond
	start := time.Now()
	if err := finishGitCmd(cmd, []string{"probe"}); err != nil {
		t.Fatalf("finishGitCmd = %v, want nil (the command exited 0)", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("waited %v — WaitDelay did not cut the inherited pipe short", elapsed)
	}
}

func TestResetHardRejectsNonSHA(t *testing.T) {
	requireGitBin(t)
	// docs.lock is a writable file and a merge-driver target: a value starting
	// with "-" must never reach git as an argument.
	err := ResetHard(dirtyRepo(t), "--hard=/etc/passwd")
	if err == nil || !strings.Contains(err.Error(), "not a commit sha") {
		t.Fatalf("ResetHard(non-sha) = %v, want a rejection", err)
	}
}

// `--` ends options but leaves pathspec magic on, so a file whose NAME contains
// glob metacharacters must still be committed literally, exactly as go-git does.
func TestCommitPathsGlobNameBackendsAgree(t *testing.T) {
	requireGitBin(t)
	const glob, decoy = "a[1].md", "a1.md"
	seed := func() string {
		t.Helper()
		dir := initRepoWith(t, map[string]string{glob: "antes\n", decoy: "isca\n"})
		writeFile(t, dir, glob, "depois\n")
		writeFile(t, dir, decoy, "isca mexida\n")
		return dir
	}
	commit := func(dir string) []Change {
		t.Helper()
		if _, err := CommitPaths(dir, []string{glob}, "probe"); err != nil {
			t.Fatalf("CommitPaths: %v", err)
		}
		left, err := WorktreeChanges(dir)
		if err != nil {
			t.Fatalf("WorktreeChanges: %v", err)
		}
		return left
	}
	execLeft := commit(seed())
	dir := seed()
	withoutGitBin(t)
	gogitLeft := commit(dir)
	// The decoy must survive uncommitted in BOTH backends: committing it would
	// mean the glob name was expanded.
	want := []Change{{Path: decoy}}
	if !reflect.DeepEqual(gogitLeft, want) {
		t.Fatalf("go-git left = %+v, want %+v", gogitLeft, want)
	}
	if !reflect.DeepEqual(execLeft, gogitLeft) {
		t.Fatalf("exec left = %+v, want %+v", execLeft, gogitLeft)
	}
}

// sshMuxEnv never reaches a test that uses a local remote (no "@" in the URL),
// so its guards are pinned directly: each one must degrade to "no multiplexing"
// rather than to a GIT_SSH_COMMAND that breaks every push.
func TestSSHMuxEnvGuards(t *testing.T) {
	dir := initRepoWith(t, map[string]string{"a.md": "a\n"})
	if got := sshMuxEnv(dir); got != "" {
		t.Fatalf("sshMuxEnv on a repo with no remote = %q, want empty", got)
	}
	t.Setenv("CERNE_NO_SSH_MUX", "1")
	if got := sshMuxEnv(dir); got != "" {
		t.Fatalf("sshMuxEnv with the kill switch = %q, want empty", got)
	}
	t.Setenv("CERNE_NO_SSH_MUX", "")
	t.Setenv("GIT_SSH_COMMAND", "ssh -i /custom/key")
	if got := sshMuxEnv(dir); got != "" {
		t.Fatalf("sshMuxEnv must not shadow the user's GIT_SSH_COMMAND, got %q", got)
	}
}

func TestIsHexSHAAcceptsBothCases(t *testing.T) {
	for _, ok := range []string{"0123456", "abcdef0123456789abcdef0123456789abcdef01", "ABCDEF0123456789ABCDEF0123456789ABCDEF01"} {
		if !isHexSHA(ok) {
			t.Fatalf("isHexSHA(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "abc", "--hard=/etc/passwd", "zzzzzzz", strings.Repeat("a", 65)} {
		if isHexSHA(bad) {
			t.Fatalf("isHexSHA(%q) = true, want false", bad)
		}
	}
}

// GIT_LITERAL_PATHSPECS in the environment would make ":(literal)" part of the
// filename instead of magic, so every add matches nothing and every commit
// fails. It is scrubbed; this pins that, along with the sibling flags.
func TestCommitPathsSurvivesInheritedPathspecEnv(t *testing.T) {
	requireGitBin(t)
	for _, env := range []string{"GIT_LITERAL_PATHSPECS", "GIT_NOGLOB_PATHSPECS", "GIT_ICASE_PATHSPECS"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, "1")
			dir := dirtyRepo(t)
			if _, err := CommitPaths(dir, []string{"edit.md", "added.md", "gone.md"}, "probe"); err != nil {
				t.Fatalf("CommitPaths with %s set: %v", env, err)
			}
			left, err := WorktreeChanges(dir)
			if err != nil {
				t.Fatalf("WorktreeChanges: %v", err)
			}
			if len(left) != 0 {
				t.Fatalf("worktree not clean: %+v", left)
			}
		})
	}
}

// A repository configured the Windows way (core.autocrlf=true) must still
// produce the same blob through either backend — git normalizes CRLF on add,
// go-git never converts.
func TestCommitPathsIgnoresAutocrlf(t *testing.T) {
	requireGitBin(t)
	blobOf := func(dir string) string {
		t.Helper()
		// Set through the binary directly: gitRun would be disabled in the
		// go-git half of this very test.
		real, err := exec.LookPath("git")
		if err != nil {
			t.Skip("no git on PATH")
		}
		if out, err := exec.Command(real, "-C", dir, "config", "core.autocrlf", "true").CombinedOutput(); err != nil {
			t.Fatalf("config: %v %s", err, out)
		}
		writeFile(t, dir, "crlf.md", "linha um\r\nlinha dois\r\n")
		if _, err := CommitPaths(dir, []string{"crlf.md"}, "probe"); err != nil {
			t.Fatalf("CommitPaths: %v", err)
		}
		blob, ok, err := FileBlobAt(dir, headOf(t, dir), "crlf.md")
		if err != nil || !ok {
			t.Fatalf("FileBlobAt: ok=%v err=%v", ok, err)
		}
		return string(blob)
	}
	execBlob := blobOf(initRepoWith(t, map[string]string{"a.md": "a\n"}))
	dir := initRepoWith(t, map[string]string{"a.md": "a\n"})
	withoutGitBin(t)
	if gogitBlob := blobOf(dir); gogitBlob != execBlob {
		t.Fatalf("blob diverged under core.autocrlf: exec=%q go-git=%q", execBlob, gogitBlob)
	}
}
