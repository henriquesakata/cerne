// Package gitback syncs a store backend directory with a git remote.
// The branch is always main, regardless of any global git configuration.
package gitback

import (
	"errors"
	"fmt"
	"io"
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

// originURL returns the first URL configured for the origin remote of repo.
func originURL(repo *git.Repository) (string, error) {
	remote, err := repo.Remote(remoteName)
	if err != nil {
		return "", err
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return "", fmt.Errorf("remote %q has no URL", remoteName)
	}
	return urls[0], nil
}

// Clone clones url (which may be a local path) into workdir on branch main.
// An empty remote still yields a usable working copy wired to origin, with
// HEAD pointing at the unborn main branch.
func Clone(url, workdir string) error {
	auth, err := sshAuthFor(url)
	if err != nil {
		return err
	}
	_, err = git.PlainClone(workdir, false, &git.CloneOptions{
		URL:           url,
		ReferenceName: mainBranch,
		Auth:          auth,
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
	url, err := originURL(repo)
	if err != nil {
		return "", err
	}
	auth, err := sshAuthFor(url)
	if err != nil {
		return "", err
	}
	err = repo.Push(&git.PushOptions{
		RemoteName: remoteName,
		RefSpecs:   []config.RefSpec{config.RefSpec("refs/heads/main:refs/heads/main")},
		Auth:       auth,
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
	url, err := originURL(repo)
	if err != nil {
		return err
	}
	auth, err := sshAuthFor(url)
	if err != nil {
		return err
	}
	err = wt.Pull(&git.PullOptions{
		RemoteName:    remoteName,
		ReferenceName: mainBranch,
		Auth:          auth,
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
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return "", fmt.Errorf("remote %q has no URL", remoteName)
	}
	auth, err := sshAuthFor(urls[0])
	if err != nil {
		return "", err
	}
	refs, err := remote.List(&git.ListOptions{Auth: auth})
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

// RemoteURL returns the configured URL of origin in workdir; an error when the
// directory is not a git repository or has no origin remote.
func RemoteURL(workdir string) (string, error) {
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		return "", err
	}
	remote, err := repo.Remote(remoteName)
	if err != nil {
		return "", err
	}
	urls := remote.Config().URLs
	if len(urls) == 0 {
		return "", fmt.Errorf("remote %q has no URL", remoteName)
	}
	return urls[0], nil
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

// LastCommitTimes returns, for each slash-separated path (relative to the repo
// root), the committer time of the most recent commit that modified it — its
// blob differs from the same path in the commit's first parent — resolved in a
// single committer-time-ordered walk from HEAD that stops once every path is
// found. Paths with no history are absent from the result; ErrNoCommits when
// HEAD is unborn. It compares blob hashes per path instead of diffing whole
// trees, which is what keeps it fast over long histories (go-git's per-commit
// FileName diff is ~35x slower). Git commit times have one-second resolution.
func LastCommitTimes(workdir string, paths []string) (map[string]time.Time, error) {
	repo, err := git.PlainOpen(workdir)
	if err != nil {
		return nil, err
	}
	head, err := repo.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrNoCommits, workdir)
	}
	if err != nil {
		return nil, err
	}
	iter, err := repo.Log(&git.LogOptions{From: head.Hash(), Order: git.LogOrderCommitterTime})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	result := make(map[string]time.Time, len(paths))
	pending := make(map[string]bool, len(paths))
	for _, p := range paths {
		pending[p] = true
	}
	for len(pending) > 0 {
		c, err := iter.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		tree, err := c.Tree()
		if err != nil {
			return nil, err
		}
		var parent *object.Tree
		if c.NumParents() > 0 {
			if p, err := c.Parent(0); err == nil {
				parent, _ = p.Tree()
			}
		}
		for path := range pending {
			h, ok := treeEntryHash(tree, path)
			if !ok {
				continue // absent here: a newer commit modified it, or it never existed
			}
			if ph, pok := treeEntryHash(parent, path); !pok || ph != h {
				result[path] = c.Committer.When // created or changed at this commit
				delete(pending, path)
			}
		}
	}
	return result, nil
}

// treeEntryHash returns the blob hash of a slash-separated path within tree; a
// nil tree (e.g. a root commit's missing parent) or a missing entry is absent.
func treeEntryHash(tree *object.Tree, path string) (plumbing.Hash, bool) {
	if tree == nil {
		return plumbing.ZeroHash, false
	}
	e, err := tree.FindEntry(path)
	if err != nil {
		return plumbing.ZeroHash, false
	}
	return e.Hash, true
}
