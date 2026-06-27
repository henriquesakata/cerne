package gitback

import (
	"errors"
	"fmt"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
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

// Fetch downloads new objects and updates remote-tracking refs from origin,
// without touching the worktree or HEAD. Already up-to-date is not an error.
func Fetch(workdir string) error {
	repo, err := openRepo(workdir)
	if err != nil {
		return err
	}
	url, err := originURL(repo)
	if err != nil {
		return err
	}
	auth, err := sshAuthFor(url)
	if err != nil {
		return err
	}
	err = repo.Fetch(&git.FetchOptions{RemoteName: remoteName, Auth: auth})
	if errors.Is(err, git.NoErrAlreadyUpToDate) || errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return nil
	}
	return err
}
