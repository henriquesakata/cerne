package gauntlet

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNoBase means no base ref could be found for the diff.
var ErrNoBase = errors.New("no diff base found (origin/<pin_branch>, origin/HEAD and main are all missing); pass --base <ref>")

// ErrNotGitRepo means the project directory is not inside a git work tree.
var ErrNotGitRepo = errors.New("not a git repository")

// ErrBadBase means the explicit --base does not name a commit.
var ErrBadBase = errors.New("base ref not found")

// defaultExcludes are paths that are noise for a critic: the embedded UI build
// and the docs pin.
var defaultExcludes = []string{"ui/dist", "docs.lock"}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// resolveBase validates dir as a git work tree and resolves base: an explicit
// one must name a commit; an empty one is the first that exists of
// origin/<pinBranch>, origin/HEAD, main; none → ErrNoBase.
func resolveBase(dir, base, pinBranch string) (string, error) {
	if out, e := runGit(dir, "rev-parse", "--is-inside-work-tree"); e != nil || strings.TrimSpace(out) != "true" {
		return "", ErrNotGitRepo
	}
	if base != "" {
		if _, e := runGit(dir, "rev-parse", "--verify", "--quiet", base+"^{commit}"); e != nil {
			return "", fmt.Errorf("%w: %s", ErrBadBase, base)
		}
		return base, nil
	}
	cands := []string{"origin/HEAD", "main"}
	if pinBranch != "" {
		cands = append([]string{"origin/" + pinBranch}, cands...)
	}
	for _, c := range cands {
		if _, e := runGit(dir, "rev-parse", "--verify", "--quiet", c+"^{commit}"); e == nil {
			return c, nil
		}
	}
	return "", ErrNoBase
}

func excludeArgs(excludes []string) []string {
	var out []string
	for _, e := range append(append([]string{}, defaultExcludes...), excludes...) {
		out = append(out, ":!"+e)
	}
	return out
}

// Diff returns `git diff <base>...HEAD` in dir minus the default and extra
// excludes, and the base ref used (see resolveBase).
func Diff(dir, base, pinBranch string, excludes []string) (diff, usedBase string, err error) {
	base, err = resolveBase(dir, base, pinBranch)
	if err != nil {
		return "", "", err
	}
	args := append([]string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", base + "...HEAD", "--", "."}, excludeArgs(excludes)...)
	out, err := runGit(dir, args...)
	return out, base, err
}

// ErrNoFeatureCommits means no non-merge commit on the base cites the id.
var ErrNoFeatureCommits = errors.New("no commit cites the feature in its message")

type noFeatureCommitsError struct{ base, id string }

func (e noFeatureCommitsError) Error() string {
	return "no commit on " + e.base + " cites " + e.id + " in its message"
}

func (e noFeatureCommitsError) Is(target error) bool { return target == ErrNoFeatureCommits }

// Commit is a feature commit as listed on stderr: short sha and subject.
type Commit struct{ Sha, Subject string }

// citesID reports whether msg holds id as a whole word: before it the start or
// a char that is no letter, digit or hyphen; after it the end or a non-digit.
func citesID(msg, id string) bool {
	for off := 0; ; {
		i := strings.Index(msg[off:], id)
		if i < 0 {
			return false
		}
		start, end := off+i, off+i+len(id)
		okBefore := start == 0 || !isIDChar(msg[start-1])
		okAfter := end == len(msg) || msg[end] < '0' || msg[end] > '9'
		if okBefore && okAfter {
			return true
		}
		off = start + 1
	}
}

// isIDChar: a char that glues to an id before it (branch and URL paths included).
func isIDChar(c byte) bool {
	return c == '-' || c == '/' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// FeatureCommits returns, oldest first, the diff of every non-merge commit
// reachable from the base whose message cites id as a whole word, each opened
// by `commit <short sha> <subject>`, minus the default and extra excludes; the
// commits themselves; and the base used. None → ErrNoFeatureCommits.
func FeatureCommits(dir, base, pinBranch, id string, excludes []string) (diff string, commits []Commit, usedBase string, err error) {
	base, err = resolveBase(dir, base, pinBranch)
	if err != nil {
		return "", nil, "", err
	}
	log, err := runGit(dir, "log", "--no-merges", "--reverse", "--format=%H%x00%s%x00%B%x1e", base)
	if err != nil {
		return "", nil, "", err
	}
	var b strings.Builder
	for _, rec := range strings.Split(log, "\x1e") {
		f := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x00", 3)
		if len(f) < 3 || !citesID(f[2], id) {
			continue
		}
		args := append([]string{"show", "--no-color", "--no-ext-diff", "--no-textconv", "--format=commit %h %s", f[0], "--", "."}, excludeArgs(excludes)...)
		out, err := runGit(dir, args...)
		if err != nil {
			return "", nil, "", err
		}
		b.WriteString(out)
		short := strings.Fields(out)[1]
		commits = append(commits, Commit{Sha: short, Subject: f[1]})
	}
	if len(commits) == 0 {
		return "", nil, "", noFeatureCommitsError{base, id}
	}
	return b.String(), commits, base, nil
}
