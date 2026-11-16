package gitback

import "github.com/go-git/go-git/v5/plumbing/object"

func ProjectTree(dir string) (*object.Tree, error) {
	repo, err := openRepo(dir)
	if err != nil {
		return nil, err
	}
	head, err := repo.Head()
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}
	return commit.Tree()
}
