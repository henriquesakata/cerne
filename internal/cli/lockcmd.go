package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
)

func newLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Manage the docs.lock snapshot pin",
	}
	cmd.AddCommand(newLockPinCmd())
	cmd.AddCommand(newLockMergeCmd())
	return cmd
}

func newLockPinCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "pin",
		Short: "Pin docs.lock to the current documentation backend HEAD",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLockPin(cmd, dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

func runLockPin(cmd *cobra.Command, dir string) error {
	// Now, not the eliding refresh: the pin is a durable committed artifact of
	// the backend head, so it must be written against a backend we just reached.
	ctx, err := resolveProjectNow(dir)
	if err != nil {
		return err
	}
	head, err := gitback.HeadSHA(ctx.repoDir)
	if err != nil {
		return fmt.Errorf("reading documentation backend HEAD: %w", err)
	}
	lock, err := project.LoadLock(ctx.projDir)
	if err != nil {
		return err
	}
	if lock.Snapshot == head {
		fmt.Fprintf(cmd.OutOrStdout(), "docs.lock already pinned at %s.\n", shortSHA(head))
		return nil
	}
	lock.Snapshot = head
	if err := project.SaveLock(ctx.projDir, lock); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Pinned docs.lock at %s.\n", shortSHA(head))
	return nil
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
// advanced (descendant) backend commit. Resolution is local-first (F-0180):
// the cache clone usually already holds both pins, so the common merge needs
// no network — the fetch (and its SSH dependency, absent in GUI-spawned
// environments) only runs when a snapshot is missing locally. Neither being
// an ancestor of the other is a real divergence and yields an error.
func descendantSnapshot(dir, ours, theirs string) (string, error) {
	ctx, err := resolvePaths(dir)
	if err != nil {
		return "", fmt.Errorf("docs.lock merge: %w", err)
	}
	winner, err := resolveDescendant(ctx.repoDir, ours, theirs)
	if err == nil || !gitback.IsUnknownCommit(err) {
		return winner, err
	}
	// A snapshot is missing locally (e.g. a pin pushed from another machine):
	// fetch once and retry.
	if ferr := gitback.Fetch(ctx.repoDir); ferr != nil {
		return "", fmt.Errorf("docs.lock merge: fetch backend: %w", ferr)
	}
	return resolveDescendant(ctx.repoDir, ours, theirs)
}

// resolveDescendant compares the two snapshots against the local cache clone
// only — the caller decides whether an unknown commit warrants a fetch.
func resolveDescendant(repoDir, ours, theirs string) (string, error) {
	oursIsAncestor, err := gitback.IsAncestor(repoDir, ours, theirs)
	if err != nil {
		return "", fmt.Errorf("docs.lock merge: %w", err)
	}
	if oursIsAncestor {
		return theirs, nil
	}
	theirsIsAncestor, err := gitback.IsAncestor(repoDir, theirs, ours)
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
	lockMergeAttr = "docs.lock merge=cerne-lock"
	lockMergeName = "cerne docs.lock snapshot resolver"
)

// lockMergeDriverCommand returns the merge driver command to register in git
// config. The binary path is absolute: git invokes the driver from whatever
// environment the merge runs in, and GUI clients/IDEs don't share the user's
// shell PATH — a PATH-relative `cerne` fails there ("command not found") and
// every docs.lock merge degrades to a manual conflict (F-0164). Quoted so
// paths with spaces survive the shell; the config is local and re-registered
// on every init/pull, so a moved binary self-heals on the next cerne run.
func lockMergeDriverCommand() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "cerne lock merge %O %A %B"
	}
	return `"` + exe + `" lock merge %O %A %B`
}

// ensureLockMergeDriver wires docs.lock to the cerne-lock merge driver in
// projDir: the .gitattributes line (committed, travels with the repo) plus the
// merge.cerne-lock.* keys in the local git config (which never travel, so init
// and pull both call this to self-heal fresh clones). Idempotent. When projDir
// is not a git repository yet it is a graceful no-op — a later pull, once the
// repo exists, registers it.
func ensureLockMergeDriver(projDir string) error {
	// EnableDotGitCommonDir resolves a worktree's shared config (.git is a file
	// there, not a dir) so the driver registers for parallel worktrees too —
	// exactly the conflict-prone workflow this case targets.
	repo, err := git.PlainOpenWithOptions(projDir, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
	if errors.Is(err, git.ErrRepositoryNotExists) {
		return nil
	}
	// go-git validates extensions when opening and refuses any repo whose config
	// declares extensions.worktreeConfig (it doesn't implement that extension).
	// Parallel git worktrees set exactly that on the shared config, so go-git can
	// never open them for a config write — the open fails before we reach
	// SetConfig. Fall back to the git binary, which writes these non-worktree
	// merge.* keys to the shared config: precisely where a merge driver must live
	// to be visible from every worktree.
	if gitback.IsUnsupportedExtension(err) {
		if err := ensureGitAttributes(projDir); err != nil {
			return err
		}
		return registerMergeDriverViaGit(projDir)
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
	sec.SetOption("name", lockMergeName)
	sec.SetOption("driver", lockMergeDriverCommand())
	return repo.SetConfig(cfg)
}

// registerMergeDriverViaGit registers the cerne-lock merge driver with the git
// binary. It is the fallback for repos go-git refuses to open for a config
// write (a worktree whose shared config declares extensions.worktreeConfig).
// git resolves the right config for these non-worktree keys — the shared one,
// which every worktree reads — so a single registration covers all of them.
func registerMergeDriverViaGit(projDir string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git binary unavailable to register the docs.lock merge driver: %w", err)
	}
	for _, kv := range [][2]string{
		{"merge.cerne-lock.name", lockMergeName},
		{"merge.cerne-lock.driver", lockMergeDriverCommand()},
	} {
		out, err := exec.Command("git", "-C", projDir, "config", kv[0], kv[1]).CombinedOutput()
		if err != nil {
			return fmt.Errorf("git config %s: %w: %s", kv[0], err, strings.TrimSpace(string(out)))
		}
	}
	return nil
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
