// Package gitback syncs a store backend directory with a git remote.
// The branch is always main, regardless of any global git configuration.
package gitback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
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
	// ErrNotRepo is the sentinel for CommitPath when workdir is not a git repo.
	ErrNotRepo = errors.New("not a git repository")
	// ErrOtherStaged is the sentinel for CommitPath when a path other than the
	// target is already staged — committing would sweep it into the pin.
	ErrOtherStaged = errors.New("other staged changes present")
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
	auth, err := authFor(url)
	if err != nil {
		return err
	}
	_, err = git.PlainClone(workdir, false, &git.CloneOptions{
		URL:           url,
		ReferenceName: mainBranch,
		Auth:          auth,
	})
	if !errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return wrapAuthHint(err, url)
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

// CommitAll stages all changes (adds, modifications, deletions) and commits
// with msg as "cerne" <cerne@local>, WITHOUT pushing. Returns the commit SHA
// hex; ErrNothingToCommit when the worktree is clean. Pushing is split out
// (Push) so the reconcile loop can commit locally, then fetch/rebase/push with
// retry on a concurrent advance.
func CommitAll(workdir, msg string) (string, error) {
	repo, err := openRepo(workdir)
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
	return hash.String(), nil
}

// commitPathsHookAfterAdd, when non-nil, runs between staging and the status
// check in CommitPaths. It exists for tests to inject a concurrent write into
// the shared worktree and pin the race window of F-0121; production leaves it
// nil.
var commitPathsHookAfterAdd func()

// CommitPaths stages and commits EXACTLY paths (modifications, additions and
// deletions) in workdir's repository, with no push — the reconcile commit
// primitive (F-0121). Unlike CommitAll it never sweeps the rest of the
// worktree, so a concurrent process's uncommitted writes in the shared clone
// cannot leak into this commit. ErrNothingToCommit when no declared path
// differs from HEAD; go-git's raw ErrEmptyCommit is mapped to the same
// sentinel, so the generic "cannot create empty commit" never escapes to a
// caller after the guard.
func CommitPaths(workdir string, paths []string, msg string) (string, error) {
	if gitBin() != "" {
		return execCommitPaths(workdir, paths, msg)
	}
	repo, err := openRepo(workdir)
	if err != nil {
		return "", err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	for _, p := range paths {
		if _, err := wt.Add(p); err != nil {
			return "", fmt.Errorf("stage %s: %w", p, err)
		}
	}
	if commitPathsHookAfterAdd != nil {
		commitPathsHookAfterAdd()
	}
	status, err := wt.Status()
	if err != nil {
		return "", err
	}
	staged := false
	for _, p := range paths {
		if s := status.File(p).Staging; s != git.Unmodified && s != git.Untracked {
			staged = true
			break
		}
	}
	if !staged {
		return "", fmt.Errorf("%w: %s", ErrNothingToCommit, workdir)
	}
	sig := &object.Signature{Name: "cerne", Email: "cerne@local", When: time.Now()}
	hash, err := wt.Commit(msg, &git.CommitOptions{Author: sig, Committer: sig})
	if errors.Is(err, git.ErrEmptyCommit) {
		return "", fmt.Errorf("%w: %s", ErrNothingToCommit, workdir)
	}
	if err != nil {
		return "", err
	}
	return hash.String(), nil
}

// Push pushes main to origin. NoErrAlreadyUpToDate is not an error. A
// non-fast-forward rejection (the remote advanced) is returned as-is so callers
// can classify it with IsNonFastForward and retry.
func Push(workdir string) error {
	if netFastPath() {
		return execPushRefspec(workdir, "refs/heads/main:refs/heads/main")
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
	err = runWithNetTimeout("push", func(ctx context.Context) error {
		return repo.PushContext(ctx, &git.PushOptions{
			RemoteName: remoteName,
			RefSpecs:   []config.RefSpec{config.RefSpec("refs/heads/main:refs/heads/main")},
			Auth:       auth,
		})
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		err = nil
	}
	if err == nil {
		markSynced(workdir) // F-0156: the elision predicate
	}
	return wrapAuthHint(err, url)
}

// PushBranch pushes refs/heads/<branch> to origin — the pin-publication
// primitive (F-0180): the user project's docs.lock pin lives on the project's
// pin_branch, not on the backend's fixed main. NoErrAlreadyUpToDate is not an
// error; a non-fast-forward rejection is returned as-is so callers can
// classify it with IsNonFastForward.
func PushBranch(workdir, branch string) error {
	if netFastPath() {
		return execPushRefspec(workdir, fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
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
	spec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
	err = runWithNetTimeout("push", func(ctx context.Context) error {
		return repo.PushContext(ctx, &git.PushOptions{
			RemoteName: remoteName,
			RefSpecs:   []config.RefSpec{spec},
			Auth:       auth,
		})
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		err = nil
	}
	if err == nil {
		markSynced(workdir)
	}
	return wrapAuthHint(err, url)
}

// CommitAndPush stages all changes, commits, and pushes main to origin in one
// step. Returns the commit SHA hex; ErrNothingToCommit when the worktree is
// clean. Retained for callers that do not need the reconcile loop.
func CommitAndPush(workdir, msg string) (string, error) {
	sha, err := CommitAll(workdir, msg)
	if err != nil {
		return "", err
	}
	if err := Push(workdir); err != nil {
		return "", err
	}
	return sha, nil
}

// RemoteHeadSHA returns the commit hex origin's main branch points at,
// without fetching or touching the local worktree; ErrNoCommits when the
// remote has no main branch yet.
func RemoteHeadSHA(workdir string) (string, error) {
	repo, err := openRepo(workdir)
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
	auth, err := authFor(urls[0])
	if err != nil {
		return "", err
	}
	refs, err := remote.List(&git.ListOptions{Auth: auth})
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return "", fmt.Errorf("%w: %s", ErrNoCommits, workdir)
	}
	if err != nil {
		return "", wrapAuthHint(err, urls[0])
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
	repo, err := openRepo(workdir)
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

// TrackingHeadSHA returns the locally-known origin/main commit hex (the
// remote-tracking ref last set by Clone/Fetch), without any network call.
// ErrNoCommits when the tracking ref does not exist yet (never fetched, or an
// empty remote).
func TrackingHeadSHA(workdir string) (string, error) {
	repo, err := openRepo(workdir)
	if err != nil {
		return "", err
	}
	ref, err := repo.Reference(plumbing.NewRemoteReferenceName(remoteName, "main"), true)
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return "", fmt.Errorf("%w: %s", ErrNoCommits, workdir)
	}
	if err != nil {
		return "", err
	}
	return ref.Hash().String(), nil
}

// FileBlobAt returns the content of relPath (slash-separated) at commit sha.
// The bool is false (with a nil error) when the commit exists but the path does
// not, or when the commit object is absent locally — both are "no such blob",
// which the 3-way reconcile reads as "this side does not have the file".
func FileBlobAt(workdir, sha, relPath string) ([]byte, bool, error) {
	repo, err := openRepo(workdir)
	if err != nil {
		return nil, false, err
	}
	commit, err := repo.CommitObject(plumbing.NewHash(sha))
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, false, err
	}
	f, err := tree.File(relPath)
	if errors.Is(err, object.ErrFileNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	data, err := f.Contents()
	if err != nil {
		return nil, false, err
	}
	return []byte(data), true, nil
}

// ResetHard sets the worktree, index and HEAD (the main branch) to commit sha,
// restoring tracked files AND removing untracked ones (go-git's HardReset
// matches the tree exactly). The reconcile loop uses it to land the worktree on
// the latest remote — fully clean — before re-applying the captured changes.
func ResetHard(workdir, sha string) error {
	if gitBin() != "" {
		return execResetHard(workdir, sha)
	}
	repo, err := openRepo(workdir)
	if err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	return wt.Reset(&git.ResetOptions{Commit: plumbing.NewHash(sha), Mode: git.HardReset})
}

// Change is one worktree path that differs from HEAD (a modification/addition,
// or a deletion).
type Change struct {
	Path    string // slash-separated, relative to the worktree root
	Deleted bool
}

// WorktreeChanges returns the paths that differ from HEAD, sorted by path. It
// captures a caller's uncommitted mutation (entity JSON, appended events, doc
// content) so the reconcile loop can re-apply it onto the latest remote.
func WorktreeChanges(workdir string) ([]Change, error) {
	if gitBin() != "" {
		return execWorktreeChanges(workdir)
	}
	repo, err := openRepo(workdir)
	if err != nil {
		return nil, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, err
	}
	status, err := wt.Status()
	if err != nil {
		return nil, err
	}
	var changes []Change
	for p, s := range status {
		if s.Staging == git.Unmodified && s.Worktree == git.Unmodified {
			continue
		}
		deleted := s.Worktree == git.Deleted || s.Staging == git.Deleted
		changes = append(changes, Change{Path: p, Deleted: deleted})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// IsNonFastForward reports whether err is a push rejection because the remote
// advanced. go-git returns this as a dynamic error ("non-fast-forward update:
// …"), not a sentinel, so this matches by string as well as the pull-side
// sentinel.
func IsNonFastForward(err error) bool {
	return err != nil && (errors.Is(err, git.ErrNonFastForwardUpdate) ||
		strings.Contains(err.Error(), "non-fast-forward"))
}

// HeadSHA returns the current HEAD commit hex of workdir; ErrNoCommits when unborn.
func HeadSHA(workdir string) (string, error) {
	repo, err := openRepo(workdir)
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

// HeadCommit returns HEAD's commit hex and committer time, so a caller can
// state how old the data it is serving is; ErrNoCommits when unborn.
func HeadCommit(workdir string) (string, time.Time, error) {
	repo, err := openRepo(workdir)
	if err != nil {
		return "", time.Time{}, err
	}
	ref, err := repo.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return "", time.Time{}, fmt.Errorf("%w: %s", ErrNoCommits, workdir)
	}
	if err != nil {
		return "", time.Time{}, err
	}
	c, err := repo.CommitObject(ref.Hash())
	if err != nil {
		return "", time.Time{}, err
	}
	return ref.Hash().String(), c.Committer.When, nil
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
	repo, err := openRepo(workdir)
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
