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

// Diff returns `git diff <base>...HEAD` in dir minus the default and extra
// excludes, and the base ref used. An empty base resolves to the first that
// exists of origin/<pinBranch>, origin/HEAD, main; none → ErrNoBase.
func Diff(dir, base, pinBranch string, excludes []string) (diff, usedBase string, err error) {
	if out, e := runGit(dir, "rev-parse", "--is-inside-work-tree"); e != nil || strings.TrimSpace(out) != "true" {
		return "", "", ErrNotGitRepo
	}
	if base != "" {
		if _, e := runGit(dir, "rev-parse", "--verify", "--quiet", base+"^{commit}"); e != nil {
			return "", "", fmt.Errorf("%w: %s", ErrBadBase, base)
		}
	}
	if base == "" {
		cands := []string{"origin/HEAD", "main"}
		if pinBranch != "" {
			cands = append([]string{"origin/" + pinBranch}, cands...)
		}
		for _, c := range cands {
			if _, e := runGit(dir, "rev-parse", "--verify", "--quiet", c+"^{commit}"); e == nil {
				base = c
				break
			}
		}
		if base == "" {
			return "", "", ErrNoBase
		}
	}
	args := []string{"diff", "--no-color", "--no-ext-diff", "--no-textconv", base + "...HEAD", "--", "."}
	for _, e := range append(append([]string{}, defaultExcludes...), excludes...) {
		args = append(args, ":!"+e)
	}
	out, err := runGit(dir, args...)
	return out, base, err
}
