package gitback

import (
	"errors"
	"fmt"
	"sort"

	"github.com/go-git/go-git/v5/plumbing"
)

// ErrNoBaseline is the sentinel for ChangedPaths when the from/to commit cannot
// be resolved — an empty or unknown SHA, or a repo with no such history. The
// drift-suspect lint rule (F-0113) treats it as "no baseline yet" and stays
// inert rather than failing the whole lint.
var ErrNoBaseline = errors.New("no drift baseline")

// ChangedPaths returns the slash-separated paths (relative to the repo root)
// whose blob differs between the fromSHA and toSHA commits of the repo at
// repoDir — added, modified, or deleted files — sorted and de-duplicated. An
// empty or unresolvable fromSHA/toSHA yields ErrNoBaseline. It diffs the two
// commit trees directly, so it is a pure function of (repo, from, to).
func ChangedPaths(repoDir, fromSHA, toSHA string) ([]string, error) {
	if fromSHA == "" || toSHA == "" {
		return nil, fmt.Errorf("%w: empty commit sha", ErrNoBaseline)
	}
	repo, err := openRepo(repoDir)
	if err != nil {
		return nil, err
	}
	fromCommit, err := repo.CommitObject(plumbing.NewHash(fromSHA))
	if err != nil {
		return nil, fmt.Errorf("%w: from %s: %v", ErrNoBaseline, fromSHA, err)
	}
	toCommit, err := repo.CommitObject(plumbing.NewHash(toSHA))
	if err != nil {
		return nil, fmt.Errorf("%w: to %s: %v", ErrNoBaseline, toSHA, err)
	}
	fromTree, err := fromCommit.Tree()
	if err != nil {
		return nil, err
	}
	toTree, err := toCommit.Tree()
	if err != nil {
		return nil, err
	}
	changes, err := fromTree.Diff(toTree)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(changes))
	for _, c := range changes {
		name := c.To.Name
		if name == "" {
			name = c.From.Name // deletion: the path lives only on the from side
		}
		if name != "" {
			seen[name] = true
		}
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}
