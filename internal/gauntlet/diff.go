package gauntlet

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
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

// Delivery is what the risk gate reads of the checkout at a directory
// (F-0516): the ref it diffs against, the merge-base and HEAD commits, and the
// files changed between them.
type Delivery struct {
	Ref        string   // the base ref resolved (see resolveBase)
	Base, Head string   // the merge-base of Ref and HEAD, and HEAD, as SHAs
	Files      []string // changed from Base to HEAD, from the repository root, no excludes; a rename as its old and its new path
	Cited      bool     // a non-merge commit of Base..HEAD is the id's, by the rule of FeatureCommits (ownCommit)
	Dirty      []string // files that differ from HEAD (staged, unstaged or untracked, not ignored), but the project's own docs.lock and .docs/
	Prefix     string   // dir from the repository root, with a trailing slash; "" at the root
}

// ReadDelivery reads the delivery of card id checked out at dir against the
// default branch (see resolveBase).
func ReadDelivery(dir, pinBranch, id string) (Delivery, error) {
	ref, err := resolveBase(dir, "", pinBranch)
	if err != nil {
		return Delivery{}, err
	}
	d := Delivery{Ref: ref}
	if d.Base, err = runGit(dir, "merge-base", ref, "HEAD"); err != nil {
		return Delivery{}, err
	}
	if d.Head, err = runGit(dir, "rev-parse", "HEAD"); err != nil {
		return Delivery{}, err
	}
	if d.Prefix, err = runGit(dir, "rev-parse", "--show-prefix"); err != nil {
		return Delivery{}, err
	}
	d.Base, d.Head, d.Prefix = strings.TrimSpace(d.Base), strings.TrimSpace(d.Head), strings.TrimSpace(d.Prefix)
	// --no-relative: a diff.relative config would drop the files outside dir.
	out, err := runGit(dir, "diff", "--name-only", "--no-renames", "--no-relative", "-z", d.Base, d.Head)
	if err != nil {
		return Delivery{}, err
	}
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			d.Files = append(d.Files, f)
		}
	}
	log, err := runGit(dir, "log", "--no-merges", "--format=%s%x00%B%x1e", d.Base+".."+d.Head)
	if err != nil {
		return Delivery{}, err
	}
	for _, rec := range strings.Split(log, "\x1e") {
		if subject, msg, ok := strings.Cut(strings.TrimLeft(rec, "\n"), "\x00"); ok {
			own, _ := ownCommit(subject, msg, id)
			d.Cited = d.Cited || own
		}
	}
	// Untracked files count: the test marks are read from the tree.
	status, err := runGit(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return Delivery{}, err
	}
	for _, e := range strings.Split(status, "\x00") {
		// cerne's own pin and projection, at the root or the project dir, are never the delivery.
		if p := e[min(3, len(e)):]; p != "" && p != "docs.lock" && p != d.Prefix+"docs.lock" && !strings.HasPrefix(p, d.Prefix+".docs/") {
			d.Dirty = append(d.Dirty, p)
		}
	}
	return d, nil
}

// ErrNoFeatureCommits means no non-merge commit on the base cites the id.
var ErrNoFeatureCommits = errors.New("no commit cites the feature in its message")

type noFeatureCommitsError struct{ base, id string }

func (e noFeatureCommitsError) Error() string {
	return "no commit on " + e.base + " cites " + e.id + " in its message"
}

func (e noFeatureCommitsError) Is(target error) bool { return target == ErrNoFeatureCommits }

// Commit is a feature commit as listed on stderr: short sha and subject.
type Commit struct{ Sha, Subject, Other string }

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

// ownCommit is the rule that makes a commit id's: its message cites id as a
// whole word, and its subject cites id or no other feature. A commit whose
// subject names another feature while only the body mentions id belongs to
// that feature, returned as other. FeatureCommits and ReadDelivery share it.
func ownCommit(subject, msg, id string) (own bool, other string) {
	if !citesID(msg, id) {
		return false, ""
	}
	if !citesID(subject, id) {
		if other = otherFeatureID(subject, id); other != "" {
			return false, other
		}
	}
	return true, ""
}

// isIDChar: a char that glues to an id before it (branch and URL paths included).
func isIDChar(c byte) bool {
	return c == '-' || c == '/' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

var featureIDRe = regexp.MustCompile(domain.FeatureIDPrefix() + `-\d+`)

// otherFeatureID returns the first feature id in subject, as a whole word,
// that is not id; "" when there is none.
func otherFeatureID(subject, id string) string {
	for _, loc := range featureIDRe.FindAllStringIndex(subject, -1) {
		if loc[0] > 0 && isIDChar(subject[loc[0]-1]) {
			continue
		}
		if o := subject[loc[0]:loc[1]]; o != id {
			return o
		}
	}
	return ""
}

// FeatureCommits returns, oldest first, the diff of every non-merge commit
// reachable from the base whose message cites id as a whole word, each opened
// by `commit <short sha> <subject>`, minus the default and extra excludes; the
// commits themselves; the commits left out because their subject cites another
// feature while only the body cites id (Other holds that id); and the base used. None → ErrNoFeatureCommits.
func FeatureCommits(dir, base, pinBranch, id string, excludes []string) (diff string, commits, skipped []Commit, usedBase string, err error) {
	base, err = resolveBase(dir, base, pinBranch)
	if err != nil {
		return "", nil, nil, "", err
	}
	log, err := runGit(dir, "log", "--no-merges", "--reverse", "--format=%H%x00%h%x00%s%x00%B%x1e", base)
	if err != nil {
		return "", nil, nil, "", err
	}
	var b strings.Builder
	for _, rec := range strings.Split(log, "\x1e") {
		f := strings.SplitN(strings.TrimLeft(rec, "\n"), "\x00", 4)
		if len(f) < 4 {
			continue
		}
		if own, other := ownCommit(f[2], f[3], id); !own {
			if other != "" {
				skipped = append(skipped, Commit{Sha: f[1], Subject: f[2], Other: other})
			}
			continue
		}
		args := append([]string{"show", "--no-color", "--no-ext-diff", "--no-textconv", "--format=commit %h %s", f[0], "--", "."}, excludeArgs(excludes)...)
		out, err := runGit(dir, args...)
		if err != nil {
			return "", nil, nil, "", err
		}
		b.WriteString(out)
		commits = append(commits, Commit{Sha: f[1], Subject: f[2]})
	}
	if len(commits) == 0 {
		return "", nil, nil, "", noFeatureCommitsError{base, id}
	}
	return b.String(), commits, skipped, base, nil
}
