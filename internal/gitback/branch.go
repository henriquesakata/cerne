package gitback

import (
	"errors"
	"fmt"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// CurrentBranch returns the branch checked out in workdir, or "" when HEAD is
// detached — so it never equals a configured pin branch (F-0102). It reads
// HEAD unresolved, so an unborn branch (fresh init, no commits yet) still
// reports its name. Linked worktrees resolve their own per-worktree HEAD via
// openRepo's commondir handling. ErrNotRepo when workdir is not a git
// repository.
func CurrentBranch(workdir string) (string, error) {
	repo, err := openRepo(workdir)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		return "", fmt.Errorf("%w: %s", ErrNotRepo, workdir)
	}
	if err != nil {
		return "", err
	}
	ref, err := repo.Reference(plumbing.HEAD, false)
	if err != nil {
		return "", err
	}
	if ref.Type() != plumbing.SymbolicReference || !ref.Target().IsBranch() {
		return "", nil // detached HEAD
	}
	return ref.Target().Short(), nil
}
