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
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// gitBin is the resolved path of the git binary, "" when unavailable. Resolved
// once per process, so CERNE_NO_GIT_BIN is read at first use and a later
// t.Setenv cannot flip it — the tests that exercise both paths reassign this
// var instead (withoutGitBin).
var gitBin = sync.OnceValue(func() string {
	if os.Getenv("CERNE_NO_GIT_BIN") != "" {
		return ""
	}
	p, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	p = unshimmed(p)
	if p == "" || !gitVersionOK(p) {
		return ""
	}
	return p
})

// minGitMajor/minGitMinor is the floor the flags here require: `status
// --no-renames` landed in git 2.18. Below it every call would die on "unknown
// option", and there is no per-call fallback to go-git — so the check happens
// once, at resolution. It doubles as proof that the binary RUNS: presence on
// PATH is not capability, and a broken or ancient install must degrade to the
// go-git path, never to a broken command.
const (
	minGitMajor = 2
	minGitMinor = 18
)

func gitVersionOK(path string) bool {
	// Bounded: this runs inside a sync.OnceValue, so a git that hangs (a
	// corporate shim, a dead NFS mount) would hang every caller in the process
	// forever. LookPath alone never executed anything; this does, so it pays
	// for a deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	fields := strings.Fields(string(out)) // "git version 2.39.5 (Apple Git-154)"
	if len(fields) < 3 {
		return false
	}
	var major, minor int
	if _, err := fmt.Sscanf(fields[2], "%d.%d", &major, &minor); err != nil {
		return false
	}
	return major > minGitMajor || (major == minGitMajor && minor >= minGitMinor)
}

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
//
// All of that is darwin-only, and the GOOS guard is load-bearing: on Linux
// /usr/bin/git IS the real binary (Debian, Alpine, the CI image), so treating
// that path as a stub would silently disable the fast path on every Linux host
// — the exact machines this optimization matters most on.
func unshimmed(path string) string {
	if runtime.GOOS != "darwin" || path != "/usr/bin/git" {
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
	// Connection multiplexing is a unix-socket feature; on Windows the options
	// are not supported and injecting GIT_SSH_COMMAND would also shadow a GIT_SSH
	// the user configured. No mux there — the fast path itself still applies.
	// GIT_SSH_COMMAND outranks GIT_SSH and core.sshCommand, so injecting it
	// would silently replace a wrapper the user configured (a bastion, a pinned
	// identity) and turn every push into "Permission denied" with no fallback.
	// Both env forms are checked; core.sshCommand is not visible without another
	// spawn and stays a documented limit — CERNE_NO_SSH_MUX is the escape.
	if runtime.GOOS == "windows" ||
		os.Getenv("GIT_SSH_COMMAND") != "" || os.Getenv("GIT_SSH") != "" ||
		os.Getenv("CERNE_NO_SSH_MUX") != "" {
		return ""
	}
	// Keyed by the REMOTE, never by the local directory: a control socket is
	// identified by user@host:port, so two clones of the same remote should
	// share one master, and two clones of DIFFERENT remotes must never collide
	// onto one — that collision would hand a connection to the wrong host, and
	// ssh would not notice. An unreadable or non-SSH remote simply gets no
	// multiplexing (GIT_SSH_COMMAND is inert for https anyway).
	url, err := RemoteURL(workdir)
	if err != nil || !strings.Contains(url, "@") || strings.HasPrefix(url, "http") {
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
	// MkdirAll applies the mode only to directories it CREATES: an already
	// existing, group/world-writable one would void the privacy this whole move
	// was for, so it is verified rather than assumed.
	// Lstat, not Stat: a symlink pointing at someone else's 0700 directory
	// would satisfy a Stat-based check while defeating the privacy it is for.
	if fi, err := os.Lstat(dir); err != nil || !fi.Mode().IsDir() || fi.Mode().Perm()&0o077 != 0 {
		return ""
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(url))
	path := filepath.Join(dir, fmt.Sprintf("%016x", h.Sum64()))
	if len(path)+18 > 104 {
		return ""
	}
	// git runs GIT_SSH_COMMAND through the SHELL, and ssh expands % tokens
	// inside ControlPath. Quoting handles a space; a quote, a percent or a
	// backslash in the cache path cannot be made safe here, and getting it
	// wrong breaks every push with no fallback — so those turn multiplexing
	// off instead.
	if strings.ContainsAny(path, `'"%\`) {
		return ""
	}
	return "ssh -o ControlMaster=auto -o 'ControlPath=" + path + "' -o ControlPersist=60s"
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
//
// Known limit: the local operations carry no deadline of their own, matching
// the go-git path, which has none either. Most stalls are ruled out by
// construction — GIT_TERMINAL_PROMPT=0 kills the prompts, --no-gpg-sign keeps
// pinentry out, and a stale index.lock makes git exit rather than wait — but
// the list is not closed (a wedged core.fsmonitor, an askpass helper), so this
// is a documented limit, not a proof.
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
	cmd := exec.CommandContext(ctx, gitBin(), append([]string{
		"-c", "safe.directory=*",
		// go-git applies no line-ending conversion, and Git for Windows ships
		// core.autocrlf=true: the same worktree would produce a different blob
		// depending on which backend ran. Turning it off is what makes the two
		// byte-identical.
		"-c", "core.autocrlf=false",
	}, args...)...)
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
	// These change how a pathspec is READ. With GIT_LITERAL_PATHSPECS=1 the
	// ":(literal)" prefix below stops being magic and becomes part of the
	// filename, so every add matches nothing and every commit fails.
	"GIT_LITERAL_PATHSPECS", "GIT_NOGLOB_PATHSPECS", "GIT_ICASE_PATHSPECS",
	"GIT_GLOB_PATHSPECS",
	// git exports these to its children whenever it was itself invoked with
	// -c: they carry arbitrary config, so GIT_CONFIG_KEY_0=core.worktree
	// re-binds the repository through the door GIT_WORK_TREE just closed.
	"GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM",
	// These outrank `-c user.name=cerne`, so an inherited value would sign the
	// backend commit as the user — a visible divergence from the go-git path,
	// which always writes the cerne signature.
	"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_AUTHOR_DATE",
	"GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_COMMITTER_DATE",
}

// repoScopedEnvPrefixes are the indexed companions of GIT_CONFIG_COUNT.
var repoScopedEnvPrefixes = []string{"GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_"}

// scrubbedEnv is the parent environment minus repoScopedEnv.
func scrubbedEnv() []string {
	env := os.Environ()
	out := env[:0:0]
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq > 0 && (slices.Contains(repoScopedEnv, kv[:eq]) || hasAnyPrefix(kv[:eq], repoScopedEnvPrefixes)) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func finishGitCmd(cmd *exec.Cmd, args []string) error {
	err := cmd.Run()
	if err == nil {
		return nil
	}
	// A grandchild that inherited the pipes — git's credential-cache daemon is
	// the real one — can hold them open past WaitDelay, and Run() then reports
	// ErrWaitDelay even though git itself exited 0. Reading that as failure
	// would turn a push that LANDED into a retry over an advanced remote: the
	// exit status is the truth here. (The ControlPersist master started below
	// is NOT such a case: OpenSSH detaches the persistent master's stdio, and
	// the call that creates it was measured at 2.63s against 2.57s with no
	// multiplexing at all — no WaitDelay tax.)
	if errors.Is(err, exec.ErrWaitDelay) && cmd.ProcessState != nil && cmd.ProcessState.Success() {
		return nil
	}
	// git reports some refusals on stdout ("nothing to commit"), so the error
	// carries both streams. Joined with a NEWLINE, never "; ": rejectedAsStale
	// reads this output line by line, and an inline joiner would weld stderr
	// onto the front of the '!' line and hide it.
	var parts []string
	for _, w := range []any{cmd.Stderr, cmd.Stdout} {
		if t := strings.TrimSpace(builderText(w)); t != "" {
			parts = append(parts, t)
		}
	}
	msg := strings.Join(parts, "\n")
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf("git %s: %s (%w)", strings.Join(args, " "), msg, err)
}

// exitedWith reports whether err is the git process exiting with code.
func exitedWith(err error, code int) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == code
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

// execPushRefspec pushes one refspec with the git binary. A rejection because
// the remote advanced is normalized to carry "non-fast-forward" so
// IsNonFastForward classifies both backends identically — and the reconcile
// loop retries on exactly that, so misreading it costs correctness, not looks.
//
// --porcelain is what makes the reading machine-oriented: git then prints one
// line per ref with a leading status flag ('!' = rejected), instead of leaving
// the caller to grep prose. Distinguishing WHY it was rejected still needs the
// summary field, but a hook decline must not be retried like a stale base, so
// the reasons are matched explicitly rather than "any rejection".
func execPushRefspec(workdir, refspec string) error {
	err := gitRunNet(workdir, "push", "push", "--porcelain", remoteName, refspec)
	if err == nil {
		markSynced(workdir)
		pushes.Add(1) // ponytail: counts a no-op push too; gitRunNet drops the --porcelain output
		return nil
	}
	// No second substring test over the whole error: it embeds the arguments,
	// so a branch NAMED "non-fast-forward" would classify every failure as a
	// stale base and spin the reconcile retry. rejectedAsStale reads the
	// porcelain lines alone; wrapping an already-matching message twice is
	// harmless, misreading one is not.
	if rejectedAsStale(err.Error()) {
		return fmt.Errorf("non-fast-forward update: %w", err)
	}
	return err
}

// staleRejectReasons are git's summaries for "your base is behind" — the only
// rejections the reconcile loop can fix by fetching and replaying. They stay
// stable because newGitCmd pins the C locale.
var staleRejectReasons = []string{"non-fast-forward", "fetch first", "stale info"}

// rejectedAsStale reports whether the push output carries a rejected-ref line
// ('!' flag, from --porcelain) whose reason is a stale base. Reason and flag
// must be on the SAME line, so an unrelated mention elsewhere in the output
// cannot promote some other rejection into a retry.
func rejectedAsStale(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "!") {
			continue
		}
		for _, reason := range staleRejectReasons {
			if strings.Contains(line, reason) {
				return true
			}
		}
	}
	return false
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
	// Without a pathspec `git add --all --` stages the WHOLE tree and the
	// commit below then fails, leaving a dirty index behind — where go-git
	// would have touched nothing. Nothing to commit is the honest answer.
	if len(paths) == 0 {
		return "", fmt.Errorf("%w: %s", ErrNothingToCommit, workdir)
	}
	// -f because go-git's Add stages an explicitly named path even when
	// .gitignore (or the user's global core.excludesFile) covers it, while git
	// refuses. The paths here come from a worktree diff, so an ignored one is
	// already unlikely — but "unlikely" is how environment-dependent
	// divergences hide.
	args := append([]string{"add", "--all", "-f", "--"}, literalPathspecs(paths)...)
	if _, err := gitRun(workdir, args...); err != nil {
		return "", err
	}
	// Same race-window seam as the go-git path (F-0121). Without this call the
	// regression test that pins the window would silently cover only the
	// fallback, since the fast path is the default.
	if commitPathsHookAfterAdd != nil {
		commitPathsHookAfterAdd()
	}
	// Whether there is anything to commit is answered by an EXIT CODE, not by
	// git's prose: `diff --cached --quiet` exits 0 when the staged set is empty.
	// The substring check below stays only as a backstop — a wording drift
	// between git versions would otherwise turn a real failure into a silent
	// "nothing to commit", and that is the one misreading with no symptom.
	diffArgs := append([]string{"diff", "--cached", "--quiet", "--"}, literalPathspecs(paths)...)
	if _, err := gitRun(workdir, diffArgs...); err == nil {
		return "", fmt.Errorf("%w: %s", ErrNothingToCommit, workdir)
	} else if !exitedWith(err, 1) {
		// Exit 1 is "there are staged changes". Anything else is diff itself
		// failing, and reading that as "there is work" would push the failure
		// one step down instead of reporting it.
		return "", err
	}
	// Commit the INDEX, not the worktree: `commit --only <paths>` would re-read
	// the files from disk and discard what `add` just staged, so anything
	// written in between would ride along — where the go-git path commits
	// exactly the index it staged.
	//
	// Both backends therefore share one invariant they do not enforce: whatever
	// else sits in the index rides along. go-git's Worktree.Commit has always
	// committed the whole index, and the reconcile loop's ResetHard is what
	// makes the staged set precisely `paths`. Keeping the same exposure is the
	// point — diverging here would be worse than the exposure itself.
	args = []string{
		"-c", "user.name=cerne", "-c", "user.email=cerne@local",
		"commit", "--no-verify", "--no-gpg-sign", "-m", msg,
	}
	if _, err := gitRun(workdir, args...); err != nil {
		if strings.Contains(err.Error(), "nothing to commit") || strings.Contains(err.Error(), "no changes added") {
			return "", fmt.Errorf("%w: %s", ErrNothingToCommit, workdir)
		}
		return "", err
	}
	sha, err := gitRun(workdir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	// The commit landed, so a sha that does not look like one means the output
	// never reached us (builderText answers "" for a stream it cannot read).
	// Returning that silently would hand the caller an empty commit id for a
	// commit that exists — the sort of thing that surfaces three layers away.
	if !isHexSHA(sha) {
		return "", fmt.Errorf("commit %s: git printed no usable sha (%q)", workdir, sha)
	}
	return sha, nil
}

// literalPathspecs stops git from reading a path as a glob. `--` ends OPTIONS;
// it does not disable pathspec magic, so a file literally named "a[1].md" or
// "v*.md" would match something else — or nothing — where go-git's Add is
// always literal. Divergence in what gets committed is the worst possible kind.
func literalPathspecs(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = ":(literal)" + p
	}
	return out
}

// execResetHard is ResetHard via the git binary: restore tracked files to sha
// and drop untracked ones, matching go-git's HardReset. Deliberately NOT `-x`
// and NOT `-ff`: ignored files and nested repositories survive, because the
// reconcile loop wants a tree with no stray *tracked-class* content, not an
// erased directory. The sweep is repo-wide, not limited to what the reset
// touched — the clone is cerne's own cache, and a leftover untracked file from
// a crashed process must not ride along into the next commit.
func execResetHard(workdir, sha string) error {
	// The sha reaches here from docs.lock / the snapshot sidecar — files a user
	// (or a merge driver) can write. A value starting with "-" would be parsed
	// as an option, so it is validated as a hex object name before it becomes
	// an argument.
	if !isHexSHA(sha) {
		return fmt.Errorf("reset %s: %q is not a commit sha", workdir, sha)
	}
	if _, err := gitRun(workdir, "reset", "--hard", sha); err != nil {
		return err
	}
	_, err := gitRun(workdir, "clean", "-fdq")
	return err
}

func isHexSHA(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// execFetch is Fetch via the git binary.
func execFetch(workdir string) error {
	// No --prune: go-git's FetchOptions leaves Prune false, and a stale
	// remote-tracking ref surviving in one backend but not the other would make
	// ref resolution depend on which path ran.
	if err := gitRunNet(workdir, "fetch", "fetch", remoteName); err != nil {
		return err
	}
	markSynced(workdir)
	return nil
}
