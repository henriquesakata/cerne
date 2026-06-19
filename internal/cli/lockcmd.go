package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/project"
)

func newLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Plumbing for the docs.lock git merge driver",
	}
	cmd.AddCommand(newLockMergeCmd())
	return cmd
}

func newLockMergeCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:    "merge <base> <ours> <theirs>",
		Short:  "Resolve a docs.lock conflict to the descendant backend snapshot (git merge driver)",
		Args:   cobra.ExactArgs(3),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLockMerge(dir, args[1], args[2])
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

// runLockMerge implements the git merge driver for docs.lock. Git passes the
// base (%O), ours (%A) and theirs (%B) versions as files and reads the result
// back from ours (%A). Because case 0030 shrank docs.lock to the snapshot pin,
// resolution is deterministic: version and backend must match, and the
// snapshot that is the descendant backend commit wins — that is the more
// advanced state, which is what two branches pushing the same backend produce.
// A genuine divergence (neither snapshot an ancestor of the other) is refused
// so the human resolves it; git then falls back to a normal conflict.
func runLockMerge(dir, oursPath, theirsPath string) error {
	ours, err := readLockFile(oursPath)
	if err != nil {
		return err
	}
	theirs, err := readLockFile(theirsPath)
	if err != nil {
		return err
	}
	if ours.Version != theirs.Version || ours.Backend != theirs.Backend {
		return fmt.Errorf("docs.lock: version/backend divergem (%d/%s vs %d/%s) — resolva manualmente",
			ours.Version, ours.Backend, theirs.Version, theirs.Backend)
	}
	winner := ours.Snapshot
	if ours.Snapshot != theirs.Snapshot {
		winner, err = descendantSnapshot(dir, ours.Snapshot, theirs.Snapshot)
		if err != nil {
			return err
		}
	}
	resolved := project.Lock{Version: ours.Version, Backend: ours.Backend, Snapshot: winner}
	return writeLockFile(oursPath, resolved)
}

// descendantSnapshot returns whichever of the two snapshots is the more
// advanced (descendant) backend commit, fetching the cache clone first so both
// are present locally. Neither being an ancestor of the other is a real
// divergence and yields an error.
func descendantSnapshot(dir, ours, theirs string) (string, error) {
	ctx, err := resolvePaths(dir)
	if err != nil {
		return "", fmt.Errorf("docs.lock merge: %w", err)
	}
	if err := gitback.Fetch(ctx.repoDir); err != nil {
		return "", fmt.Errorf("docs.lock merge: fetch backend: %w", err)
	}
	oursIsAncestor, err := gitback.IsAncestor(ctx.repoDir, ours, theirs)
	if err != nil {
		return "", fmt.Errorf("docs.lock merge: %w", err)
	}
	if oursIsAncestor {
		return theirs, nil
	}
	theirsIsAncestor, err := gitback.IsAncestor(ctx.repoDir, theirs, ours)
	if err != nil {
		return "", fmt.Errorf("docs.lock merge: %w", err)
	}
	if theirsIsAncestor {
		return ours, nil
	}
	return "", fmt.Errorf("docs.lock: snapshots divergem (%s vs %s) — rode 'cerne pull' e resolva manualmente",
		shortSHA(ours), shortSHA(theirs))
}

func readLockFile(path string) (project.Lock, error) {
	var l project.Lock
	data, err := os.ReadFile(path)
	if err != nil {
		return l, err
	}
	if err := json.Unmarshal(data, &l); err != nil {
		return l, fmt.Errorf("%s: %w", path, err)
	}
	return l, nil
}

func writeLockFile(path string, l project.Lock) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

const (
	lockMergeAttr   = "docs.lock merge=cerne-lock"
	lockMergeDriver = "cerne lock merge %O %A %B"
)

// ensureLockMergeDriver wires docs.lock to the cerne-lock merge driver in
// projDir: the .gitattributes line (committed, travels with the repo) plus the
// merge.cerne-lock.* keys in the local git config (which never travel, so init
// and pull both call this to self-heal fresh clones). Idempotent. When projDir
// is not a git repository yet it is a graceful no-op — a later pull, once the
// repo exists, registers it.
func ensureLockMergeDriver(projDir string) error {
	repo, err := git.PlainOpen(projDir)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := ensureGitAttributes(projDir); err != nil {
		return err
	}
	cfg, err := repo.Config()
	if err != nil {
		return err
	}
	sec := cfg.Raw.Section("merge").Subsection("cerne-lock")
	sec.SetOption("name", "cerne docs.lock snapshot resolver")
	sec.SetOption("driver", lockMergeDriver)
	return repo.SetConfig(cfg)
}

// ensureGitAttributes appends the docs.lock merge line to projDir/.gitattributes
// unless an identical line is already present.
func ensureGitAttributes(projDir string) error {
	path := filepath.Join(projDir, ".gitattributes")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == lockMergeAttr {
			return nil
		}
	}
	buf := append([]byte{}, data...)
	if len(buf) > 0 && buf[len(buf)-1] != '\n' {
		buf = append(buf, '\n')
	}
	buf = append(buf, []byte(lockMergeAttr+"\n")...)
	return os.WriteFile(path, buf, 0o644)
}
