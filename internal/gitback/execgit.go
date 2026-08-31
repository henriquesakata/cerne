package gitback

// Native-git fast path. go-git is 10-30x slower than the git binary on a
// backend clone of any real size: on this project's own backend (2862 commits,
// 1541 files) pushing a single one-file commit costs ~16.5s of pure CPU even
// against a LOCAL bare repo, because go-git's push walks the entire history to
// compute the object set; the git binary does the same push in ~0.5s. Same
// story, smaller factor, for status (1.1s vs 0.09s), commit (2.4s vs 0.4s) and
// hard reset (0.8s vs 0.1s). Together they made every lifecycle transition a
// ~40s wait.
//
// So: when `git` is on PATH, shell out for those four. Everything else stays
// go-git, and a missing binary falls back to go-git too — the git binary is an
// optimization, never a requirement. CERNE_NO_GIT_BIN=1 forces the fallback
// (used by the tests that pin both paths to the same behavior).

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// gitBin is the resolved path of the git binary, "" when unavailable. It is a
// var so tests can force the go-git fallback.
var gitBin = sync.OnceValue(func() string {
	if os.Getenv("CERNE_NO_GIT_BIN") != "" {
		return ""
	}
	p, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	return unshimmed(p)
})

// macOS ships /usr/bin/git as a stub that re-resolves the active developer
// directory on every invocation: measured 118ms per spawn against 37ms for the
// binary it forwards to. A mutation spawns git a handful of times, so going
// straight to the real one is worth the two Stat calls. Not via `xcrun -f git`
// — that is a ~200ms subprocess of its own, and this is a one-shot CLI.
//
// The stub is also why LookPath alone cannot answer "is git available here":
// /usr/bin/git exists on every Mac, including one with no Command Line Tools,
// where running it aborts or pops the install dialog and blocks. So when the
// stub is all there is, this reports NO binary and the caller keeps the go-git
// path — a machine that worked before this fast path existed must keep working.
// A developer directory in a non-default location degrades the same way: no
// fast path, same behavior as before, never a broken command.
func unshimmed(path string) string {
	if path != "/usr/bin/git" {
		return path
	}
	for _, c := range []string{
		"/Library/Developer/CommandLineTools/usr/bin/git",
		"/Applications/Xcode.app/Contents/Developer/usr/bin/git",
	} {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return c
		}
	}
	return ""
}

// sshMuxEnv is the GIT_SSH_COMMAND that reuses one SSH connection across cerne
// processes. The handshake to the backend host costs ~1.2s and every command
// pays it twice (SafePull's fetch, then the push); an agent session fires those
// in bursts, so multiplexing collapses the repeats to ~0.1s.
//
// The socket lives under the user's cache directory, never the shared temp dir:
// a predictable name in a world-writable /tmp lets another local user pre-create
// the path and either deny the connection or interpose their own master. It is
// empty (no override) when the user already set GIT_SSH_COMMAND, when the cache
// directory is unusable, or when the path would not fit the ~104-byte sun_path
// limit — ssh ERRORS OUT on a long ControlPath instead of falling back, so the
// guard is not optional, and the budget must include the ~17-byte random suffix
// ssh appends while the master is being created.
func sshMuxEnv(workdir string) string {
	if os.Getenv("GIT_SSH_COMMAND") != "" || os.Getenv("CERNE_NO_SSH_MUX") != "" {
		return ""
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(cache, "cerne", "ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(workdir))
	path := filepath.Join(dir, fmt.Sprintf("%08x", h.Sum32()))
	if len(path)+18 > 104 {
		return ""
	}
	return "ssh -o ControlMaster=auto -o ControlPath=" + path + " -o ControlPersist=60s"
}

// netFastPath reports whether the network ops (push, fetch) may use the git
// binary. The local ops need no credentials, but these do — and cerne carries
// two credential sources the binary knows nothing about: CERNE_GIT_TOKEN (HTTPS)
// and CERNE_SSH_DIR (the key-file probe). With either set, the network ops stay
// on go-git, which is where that auth lives; everything else (agent keys, the
// default ~/.ssh identities, known_hosts) native ssh resolves the same way.
func netFastPath() bool {
	return gitBin() != "" && os.Getenv("CERNE_GIT_TOKEN") == "" && os.Getenv("CERNE_SSH_DIR") == ""
}

// gitRunNet is gitRun for the network operations (push, fetch), bounded by the
// same deadline as the go-git path: the context kills the child process, and
// WaitDelay stops a wedged pipe from outliving it.
func gitRunNet(workdir, name string, args ...string) error {
	return runWithNetTimeout(name, func(ctx context.Context) error {
		cmd := newGitCmd(ctx, workdir, args...)
		cmd.WaitDelay = 2 * time.Second // a wedged pipe must not outlive the kill
		if mux := sshMuxEnv(workdir); mux != "" {
			cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND="+mux)
		}
		return finishGitCmd(cmd, args)
	})
}

// gitRun runs the git binary in workdir. Stdout comes back trimmed; a non-zero
// exit becomes an error carrying stderr (git's own message is the useful one).
func gitRun(workdir string, args ...string) (string, error) {
	cmd := newGitCmd(context.Background(), workdir, args...)
	if err := finishGitCmd(cmd, args); err != nil {
		return "", err
	}
	return strings.TrimRight(builderText(cmd.Stdout), "\n"), nil
}

func newGitCmd(ctx context.Context, workdir string, args ...string) *exec.Cmd {
	// safe.directory: git >=2.35.2 refuses a repository owned by another user
	// (a clone mounted into a container, CI running as root). go-git never
	// checked ownership, and this clone is a cache directory cerne created for
	// itself — refusing it here would be a regression, not a defense.
	cmd := exec.CommandContext(ctx, gitBin(), append([]string{"-c", "safe.directory=*"}, args...)...)
	cmd.Dir = workdir
	cmd.Env = append(scrubbedEnv(),
		// Never let git ask for input (a credential or passphrase prompt would
		// hang a CLI with no terminal attached).
		"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0",
		// git translates its messages, and this package classifies control flow
		// on them ("nothing to commit" -> ErrNothingToCommit, "[rejected]" ->
		// non-fast-forward). Under a localized environment those substrings stop
		// matching and the misclassification is SILENT, so the C locale is part
		// of the contract, not tidiness.
		"LC_ALL=C", "LANGUAGE=", "LANG=C")
	cmd.Stdout, cmd.Stderr = &strings.Builder{}, &strings.Builder{}
	return cmd
}

// repoScopedEnv are the git variables that rebind a git process to ANOTHER
// repository. cerne runs inside git (the `cerne lock merge` driver wired via
// .gitattributes, and the hooks), and there these point at the user's project:
// a child `git commit` in the backend clone would then write the outer repo's
// index. go-git never read them, so inheriting them would be a silent
// divergence between the two backends — the worst kind.
var repoScopedEnv = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_NAMESPACE", "GIT_PREFIX", "GIT_INDEX_VERSION",
}

// scrubbedEnv is the parent environment minus repoScopedEnv.
func scrubbedEnv() []string {
	env := os.Environ()
	out := env[:0:0]
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq > 0 && slices.Contains(repoScopedEnv, kv[:eq]) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func finishGitCmd(cmd *exec.Cmd, args []string) error {
	err := cmd.Run()
	if err == nil {
		return nil
	}
	// git reports some refusals on stdout ("nothing to commit"), so the error
	// carries both streams — callers classify by substring.
	var parts []string
	for _, w := range []any{cmd.Stderr, cmd.Stdout} {
		if t := strings.TrimSpace(builderText(w)); t != "" {
			parts = append(parts, t)
		}
	}
	msg := strings.Join(parts, "; ")
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
}

// builderText reads back a stream newGitCmd installed, "" for anything else —
// the assertion is checked because a panic here would turn a git hiccup into a
// crash of the whole command.
func builderText(w any) string {
	if b, ok := w.(*strings.Builder); ok {
		return b.String()
	}
	return ""
}

// execPushRefspec pushes one refspec with the git binary. A rejection (the
// remote advanced) is normalized to carry "non-fast-forward" so
// IsNonFastForward classifies both backends identically.
func execPushRefspec(workdir, refspec string) error {
	err := gitRunNet(workdir, "push", "push", remoteName, refspec)
	if err == nil {
		markSynced(workdir)
		return nil
	}
	if s := err.Error(); strings.Contains(s, "[rejected]") && !strings.Contains(s, "non-fast-forward") {
		return fmt.Errorf("non-fast-forward update: %w", err)
	}
	return err
}

// execWorktreeChanges is WorktreeChanges via `git status --porcelain`.
func execWorktreeChanges(workdir string) ([]Change, error) {
	out, err := gitRun(workdir, "status", "--porcelain", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return nil, err
	}
	var changes []Change
	for _, rec := range strings.Split(out, "\x00") {
		if len(rec) < 4 { // "XY " + at least one path byte
			continue
		}
		x, y, path := rec[0], rec[1], rec[3:]
		changes = append(changes, Change{Path: path, Deleted: x == 'D' || y == 'D'})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// execCommitPaths is CommitPaths via the git binary: stage exactly paths, then
// commit them. ErrNothingToCommit when staging produced no change.
func execCommitPaths(workdir string, paths []string, msg string) (string, error) {
	args := append([]string{"add", "--all", "--"}, paths...)
	if _, err := gitRun(workdir, args...); err != nil {
		return "", err
	}
	args = []string{
		"-c", "user.name=cerne", "-c", "user.email=cerne@local",
		"commit", "--only", "--no-verify", "-m", msg, "--",
	}
	if _, err := gitRun(workdir, append(args, paths...)...); err != nil {
		if strings.Contains(err.Error(), "nothing to commit") || strings.Contains(err.Error(), "no changes added") {
			return "", fmt.Errorf("%w: %s", ErrNothingToCommit, workdir)
		}
		return "", err
	}
	return gitRun(workdir, "rev-parse", "HEAD")
}

// execResetHard is ResetHard via the git binary: match the tree exactly, which
// means dropping untracked files too (go-git's HardReset semantics).
func execResetHard(workdir, sha string) error {
	if _, err := gitRun(workdir, "reset", "--hard", sha); err != nil {
		return err
	}
	_, err := gitRun(workdir, "clean", "-fdq")
	return err
}

// execFetch is Fetch via the git binary.
func execFetch(workdir string) error {
	if err := gitRunNet(workdir, "fetch", "fetch", "--prune", remoteName); err != nil {
		return err
	}
	markSynced(workdir)
	return nil
}
