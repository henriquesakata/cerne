package gitback

import (
	"context"
	"errors"
	"fmt"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// IsAncestor reports whether commit a is an ancestor of commit b in workdir
// (a == b counts as ancestor). Both commit objects must already be present
// locally — call Fetch first when one side may have been pushed from another
// branch. The docs.lock merge driver uses this to resolve two divergent
// snapshot pins to the more advanced (descendant) backend commit.
func IsAncestor(workdir, a, b string) (bool, error) {
	repo, err := openRepo(workdir)
	if err != nil {
		return false, err
	}
	ca, err := repo.CommitObject(plumbing.NewHash(a))
	if err != nil {
		return false, fmt.Errorf("commit %s: %w", a, err)
	}
	cb, err := repo.CommitObject(plumbing.NewHash(b))
	if err != nil {
		return false, fmt.Errorf("commit %s: %w", b, err)
	}
	return ca.IsAncestor(cb)
}

// IsUnknownCommit reports whether err (from IsAncestor) means one of the
// commits is not present in the local clone — the caller may Fetch once and
// retry before treating the pins as truly divergent (F-0180).
func IsUnknownCommit(err error) bool {
	return errors.Is(err, plumbing.ErrObjectNotFound)
}

// AheadTouchesOnly reports whether every commit in origin/<branch>..HEAD
// touches only the given path — the guard that keeps pin publication (F-0180)
// from ever pushing the user's own unpublished work. False (never an error)
// when the tracking ref is missing, HEAD is not a linear descendant of it, or
// any commit in the range is a merge or touches another path.
func AheadTouchesOnly(workdir, branch, path string) (bool, error) {
	repo, err := openRepo(workdir)
	if err != nil {
		return false, err
	}
	remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName(remoteName, branch), true)
	if err != nil {
		return false, nil
	}
	headRef, err := repo.Head()
	if err != nil {
		return false, nil
	}
	cur, err := repo.CommitObject(headRef.Hash())
	if err != nil {
		return false, err
	}
	for cur.Hash != remoteRef.Hash() {
		if cur.NumParents() != 1 {
			return false, nil // merge commit, or root reached without meeting the tracking ref
		}
		parent, err := cur.Parent(0)
		if err != nil {
			return false, err
		}
		curTree, err := cur.Tree()
		if err != nil {
			return false, err
		}
		parentTree, err := parent.Tree()
		if err != nil {
			return false, err
		}
		changes, err := object.DiffTree(parentTree, curTree)
		if err != nil {
			return false, err
		}
		for _, ch := range changes {
			if (ch.From.Name != "" && ch.From.Name != path) || (ch.To.Name != "" && ch.To.Name != path) {
				return false, nil
			}
		}
		cur = parent
	}
	return true, nil
}

// Fetch downloads new objects and updates remote-tracking refs from origin,
// without touching the worktree or HEAD. Already up-to-date is not an error.
func Fetch(workdir string) error {
	if netFastPath() {
		return execFetch(workdir)
	}
	repo, err := openRepo(workdir)
	if err != nil {
		return err
	}
	url, err := originURL(repo)
	if err != nil {
		return err
	}
	auth, err := authFor(url)
	if err != nil {
		return err
	}
	err = runWithNetTimeout("fetch", func(ctx context.Context) error {
		return repo.FetchContext(ctx, &git.FetchOptions{RemoteName: remoteName, Auth: auth})
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) || errors.Is(err, transport.ErrEmptyRemoteRepository) {
		err = nil
	}
	if err == nil {
		markSynced(workdir) // F-0156: the elision predicate
	}
	return wrapAuthHint(err, url)
}
