package app

import (
	"errors"

	git "github.com/go-git/go-git/v5"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
)

// RecordProjectHead writes the user project's current git HEAD to the
// .docs/.projecthead sidecar (F-0113), the baseline the drift-suspect lint rule
// diffs against. It is inert — returns nil without writing — when projDir is not
// a git repo or has no commits yet, so a non-git project never errors a sync.
func RecordProjectHead(projDir string) error {
	sha, err := gitback.HeadSHA(projDir)
	if errors.Is(err, gitback.ErrNoCommits) || errors.Is(err, git.ErrRepositoryNotExists) {
		return nil
	}
	if err != nil {
		return err
	}
	return project.SaveProjectHead(projDir, sha)
}
