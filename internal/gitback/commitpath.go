package gitback

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CommitPath commits exactly one file of workdir — the user's project repo,
// possibly a huge monorepo — WITHOUT scanning the worktree (F-0153): all
// checks are directed (HEAD tree vs index, plus the target file's blob), so
// the cost is O(tracked files' metadata), never O(hashing the working tree).
// go-git's Worktree.Status took 80s+ per call on the hub monorepo and was
// the measured cause of 113s-average snapshot phases; TestCommitPathAvoids-
// WorktreeStatus guards against reintroducing it (plain wt.Add included —
// it calls Status internally; AddWithOptions{SkipStatus} is the sanctioned
// path).
//
// Semantics (pinned by the commitpath tests): ErrNotRepo when workdir is not
// a git repo; ErrOtherStaged when the index differs from HEAD on any OTHER
// path (modification or deletion) — the user's staged work is never swept;
// ErrNothingToCommit when the target is byte-identical to HEAD; untracked or
// unstaged changes elsewhere never block and never ride along. No push.
func CommitPath(workdir, path, msg string) (string, error) {
	repo, err := openRepo(workdir)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		return "", fmt.Errorf("%w: %s", ErrNotRepo, workdir)
	}
	if err != nil {
		return "", err
	}

	// HEAD tree as path→blob-hash; an unborn HEAD (no commits) is an empty tree.
	treeHash := map[string]plumbing.Hash{}
	if headRef, err := repo.Head(); err == nil {
		commit, err := repo.CommitObject(headRef.Hash())
		if err != nil {
			return "", err
		}
		tree, err := commit.Tree()
		if err != nil {
			return "", err
		}
		if err := tree.Files().ForEach(func(f *object.File) error {
			treeHash[f.Name] = f.Hash
			return nil
		}); err != nil {
			return "", err
		}
	} else if !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return "", err
	}

	// Staged-elsewhere check, worktree untouched: an index entry whose hash
	// differs from HEAD (or absent from HEAD: staged add), or a HEAD path
	// missing from the index (staged deletion), on any path but the target.
	idx, err := repo.Storer.Index()
	if err != nil {
		return "", err
	}
	inIndex := make(map[string]plumbing.Hash, len(idx.Entries))
	for _, e := range idx.Entries {
		inIndex[e.Name] = e.Hash
	}
	for name, h := range inIndex {
		if name == path {
			continue
		}
		if th, ok := treeHash[name]; !ok || th != h {
			return "", fmt.Errorf("%w: %s", ErrOtherStaged, name)
		}
	}
	for name := range treeHash {
		if name == path {
			continue
		}
		if _, ok := inIndex[name]; !ok {
			return "", fmt.Errorf("%w: %s", ErrOtherStaged, name)
		}
	}

	// Nothing-to-commit check: hash only the target's bytes.
	data, err := os.ReadFile(filepath.Join(workdir, filepath.FromSlash(path)))
	if err != nil {
		return "", err
	}
	if h, ok := treeHash[path]; ok && h == blobHashOf(data) {
		return "", fmt.Errorf("%w: %s", ErrNothingToCommit, path)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	// SkipStatus: stage the one file without go-git hashing the whole tree.
	if err := wt.AddWithOptions(&git.AddOptions{Path: path, SkipStatus: true}); err != nil {
		return "", err
	}
	sig := &object.Signature{Name: "cerne", Email: "cerne@local", When: time.Now()}
	hash, err := wt.Commit(msg, &git.CommitOptions{Author: sig, Committer: sig})
	if errors.Is(err, git.ErrEmptyCommit) {
		return "", fmt.Errorf("%w: %s", ErrNothingToCommit, path)
	}
	if err != nil {
		return "", err
	}
	return hash.String(), nil
}

// blobHashOf computes the git blob hash of content without writing anything.
func blobHashOf(data []byte) plumbing.Hash {
	return plumbing.ComputeHash(plumbing.BlobObject, data)
}
