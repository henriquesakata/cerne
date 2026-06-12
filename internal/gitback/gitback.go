// Package gitback syncs a store backend directory with a git remote.
// The branch is always main, regardless of any global git configuration.
package gitback

import (
	"errors"
	"fmt"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

var (
	// ErrNothingToCommit is the sentinel for a CommitAndPush with a clean worktree.
	ErrNothingToCommit = errors.New("nothing to commit")
	// ErrNoCommits is the sentinel for a HeadSHA on a repo with an unborn HEAD.
	ErrNoCommits = errors.New("no commits yet")
)

const remoteName = "origin"

var mainBranch = plumbing.NewBranchReferenceName("main")

// InitBare creates a bare repository at path with HEAD pointing at main.
func InitBare(path string) error {
	_, err := git.PlainInitWithOptions(path, &git.PlainInitOptions{
		Bare:        true,
		InitOptions: git.InitOptions{DefaultBranch: mainBranch},
	})
	return err
}

// Clone clones url (which may be a local path) into workdir on branch main.
// An empty remote still yields a usable working copy wired to origin, with
// HEAD pointing at the unborn main branch.
func Clone(url, workdir string) error {
	_, err := git.PlainClone(workdir, false, &git.CloneOptions{
		URL:           url,
		ReferenceName: mainBranch,
	})
	if !errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return err
	}
	// Empty remote: PlainClone cleaned up workdir; init fresh and wire origin.
	repo, err := git.PlainInitWithOptions(workdir, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: mainBranch},
	})
	if err != nil {
		return err
	}
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: remoteName,
		URLs: []string{url},
	})
	return err
}

// CommitAndPush stages all changes (adds, modifications, deletions), commits
// with msg as "cerne" <cerne@local> and pushes main to origin. Returns the
// commit SHA hex; ErrNothingToCommit when the worktree is clean.
func CommitAndPush(workdir, msg string) (string, error) {
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		return "", err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return "", err
	}
	status, err := wt.Status()
	if err != nil {
		return "", err
	}
	if status.IsClean() {
		return "", fmt.Errorf("%w: %s", ErrNothingToCommit, workdir)
	}
	sig := &object.Signature{Name: "cerne", Email: "cerne@local", When: time.Now()}
	hash, err := wt.Commit(msg, &git.CommitOptions{Author: sig, Committer: sig})
	if err != nil {
		return "", err
	}
	err = repo.Push(&git.PushOptions{
		RemoteName: remoteName,
		RefSpecs:   []config.RefSpec{config.RefSpec("refs/heads/main:refs/heads/main")},
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return "", err
	}
	return hash.String(), nil
}

// Pull fast-forwards workdir from origin/main. Already up-to-date is not an error.
func Pull(workdir string) error {
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	err = wt.Pull(&git.PullOptions{
		RemoteName:    remoteName,
		ReferenceName: mainBranch,
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		return nil
	}
	return err
}

// RemoteHeadSHA returns the commit hex origin's main branch points at,
// without fetching or touching the local worktree; ErrNoCommits when the
// remote has no main branch yet.
func RemoteHeadSHA(workdir string) (string, error) {
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		return "", err
	}
	remote, err := repo.Remote(remoteName)
	if err != nil {
		return "", err
	}
	refs, err := remote.List(&git.ListOptions{})
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return "", fmt.Errorf("%w: %s", ErrNoCommits, workdir)
	}
	if err != nil {
		return "", err
	}
	for _, ref := range refs {
		if ref.Name() == mainBranch {
			return ref.Hash().String(), nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrNoCommits, workdir)
}

// HeadSHA returns the current HEAD commit hex of workdir; ErrNoCommits when unborn.
func HeadSHA(workdir string) (string, error) {
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		return "", err
	}
	ref, err := repo.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return "", fmt.Errorf("%w: %s", ErrNoCommits, workdir)
	}
	if err != nil {
		return "", err
	}
	return ref.Hash().String(), nil
}
