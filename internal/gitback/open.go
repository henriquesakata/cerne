package gitback

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	billy "github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
)

const gitDirName = ".git"

// openRepo opens the git repository at dir, tolerating repos whose config
// declares extensions.worktreeConfig — which go-git refuses to open. The cause
// is a case mismatch in go-git: it lowercases the extension name to
// "worktreeconfig" but keys its allow-list as "worktreeConfig", so the match
// always fails and the open errors with "core.repositoryformatversion does not
// support extension: worktreeconfig". Parallel git worktrees (e.g. Claude
// Code's) set exactly that on the shared config, which would otherwise make
// every cerne command that touches such a repo fail (F-0075).
//
// The fast path is plain go-git. Only on the extension error do we rebuild the
// same filesystem storage with Config() wrapped to drop the extensions section
// before go-git validates it. Nothing is ever written back, so the user's real
// .git/config keeps the extension — cerne must never mutate the project's git
// config to work around this.
//
// Every git open in this package routes through openRepo; TestNoRawPlainOpen
// guards that, so a new open site can't silently reintroduce the showstopper.
func openRepo(dir string) (*git.Repository, error) {
	repo, err := git.PlainOpenWithOptions(dir, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
	if !IsUnsupportedExtension(err) {
		return repo, err
	}
	s, wt, terr := tolerantStorage(dir)
	if terr != nil {
		return nil, terr
	}
	return git.Open(s, wt)
}

// IsUnsupportedExtension reports whether err is go-git refusing a repo for an
// extension it doesn't implement — in practice extensions.worktreeConfig set by
// parallel git worktrees. lockcmd uses the same classifier to choose its
// git-binary fallback for the docs.lock merge-driver config write.
func IsUnsupportedExtension(err error) bool {
	return errors.Is(err, git.ErrUnsupportedExtensionRepositoryFormatVersion) ||
		errors.Is(err, git.ErrUnknownExtension)
}

// extensionTolerantStorer is a storage.Storer whose Config() drops the
// extensions section, so git.Open's extension validation can't refuse the repo.
// Every other method delegates unchanged. gitback never calls SetConfig through
// it, so the on-disk config (extension included) is never rewritten.
type extensionTolerantStorer struct {
	storage.Storer
}

func (s extensionTolerantStorer) Config() (*config.Config, error) {
	cfg, err := s.Storer.Config()
	if err != nil {
		return nil, err
	}
	if cfg.Raw != nil {
		cfg.Raw.RemoveSection("extensions")
	}
	return cfg, nil
}

// tolerantStorage builds the same filesystem storage PlainOpenWithOptions would,
// wrapped so the extensions section is invisible to go-git's validator. It ports
// go-git's own .git resolution (a real directory vs a linked-worktree "gitdir:"
// file, plus the worktree commondir) for the no-detect case cerne always uses —
// workdir is always an exact repo root.
func tolerantStorage(dir string) (storage.Storer, billy.Filesystem, error) {
	wt := osfs.New(dir)
	fi, err := wt.Stat(gitDirName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, git.ErrRepositoryNotExists
		}
		return nil, nil, err
	}

	var dot billy.Filesystem
	if fi.IsDir() {
		dot, err = wt.Chroot(gitDirName)
	} else {
		dot, err = dotGitFileToFS(dir, wt)
	}
	if err != nil {
		return nil, nil, err
	}

	repoFS := dot
	common, err := commonDirFS(dot)
	if err != nil {
		return nil, nil, err
	}
	if common != nil {
		repoFS = dotgit.NewRepositoryFilesystem(dot, common)
	}

	base := filesystem.NewStorage(repoFS, cache.NewObjectLRUDefault())
	return extensionTolerantStorer{base}, wt, nil
}

// dotGitFileToFS resolves a linked worktree's .git file ("gitdir: <path>") to
// the real git directory filesystem. Ported from go-git's
// dotGitFileToOSFilesystem (the no-detect path).
func dotGitFileToFS(dir string, fs billy.Filesystem) (billy.Filesystem, error) {
	f, err := fs.Open(gitDirName)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	const prefix = "gitdir: "
	line := string(b)
	if !strings.HasPrefix(line, prefix) {
		return nil, errors.New(".git file has no gitdir: prefix")
	}
	gitdir := strings.TrimSpace(strings.Split(line[len(prefix):], "\n")[0])
	if filepath.IsAbs(gitdir) {
		return osfs.New(gitdir), nil
	}
	return osfs.New(fs.Join(dir, gitdir)), nil
}

// commonDirFS resolves a worktree's shared git directory from its "commondir"
// file; nil when absent (a normal, non-worktree repo). Ported from go-git's
// dotGitCommonDirectory.
func commonDirFS(fs billy.Filesystem) (billy.Filesystem, error) {
	f, err := fs.Open("commondir")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, nil
	}
	path := strings.TrimSpace(string(b))
	var common billy.Filesystem
	if filepath.IsAbs(path) {
		common = osfs.New(path)
	} else {
		common = osfs.New(filepath.Join(fs.Root(), path))
	}
	if _, err := common.Stat(""); err != nil {
		if os.IsNotExist(err) {
			return nil, git.ErrRepositoryIncomplete
		}
		return nil, err
	}
	return common, nil
}
