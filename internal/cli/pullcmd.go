package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/index"
	"gitlab.com/rockt/cerne/internal/materialize"
	"gitlab.com/rockt/cerne/internal/project"
)

func newPullCmd() *cobra.Command {
	var dir, scope string
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Materialize the current project state into .docs/",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPull(cmd, dir, scope)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().StringVar(&scope, "scope", "", "materialize only features of this module")
	return cmd
}

func runPull(cmd *cobra.Command, dir, scope string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if scope == "" {
		scope = ctx.cfg.DefaultScope
	}
	manifest, err := materialize.Materialize(ctx.repoDir, filepath.Join(ctx.projDir, ".docs"), scope)
	if err != nil {
		return err
	}
	head, err := gitback.HeadSHA(ctx.repoDir)
	if err != nil {
		return err
	}
	lock, err := project.LoadLock(ctx.projDir)
	if errors.Is(err, project.ErrNoLock) {
		lock = project.Lock{Version: 1, Backend: ctx.cfg.Backend}
	} else if err != nil {
		return err
	}
	lock.Snapshot = head
	if err := project.SaveLock(ctx.projDir, lock); err != nil {
		return err
	}
	if err := project.SaveManifest(ctx.projDir, manifest); err != nil {
		return err
	}
	// Self-heal the merge driver: .git/config never travels in a clone, so the
	// routine sync point re-registers it. Non-fatal.
	if err := ensureLockMergeDriver(ctx.projDir); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "aviso: não foi possível registrar o merge driver do docs.lock: %v\n", err)
	}
	// pull is the sync point: refresh the local search index alongside .docs/.
	dbPath, err := project.IndexPath(ctx.cfg.ProjectID)
	if err != nil {
		return err
	}
	if err := index.Rebuild(ctx.repoDir, dbPath); err != nil {
		return err
	}
	nFeatures, nDecisions, nReferences := countManifest(manifest)
	fmt.Fprintf(cmd.OutOrStdout(),
		"Materializado: %d feature(s), %d decisão(ões), %d referência(s) em .docs/ (snapshot %s).\n",
		nFeatures, nDecisions, nReferences, head[:8])
	return nil
}

// countManifest derives the summary counts from the manifest paths:
// features count distinct current/features/<dir>/ entries, decisions and
// references count one file each.
func countManifest(manifest map[string]string) (features, decisions, references int) {
	featureDirs := map[string]bool{}
	for rel := range manifest {
		switch {
		case strings.HasPrefix(rel, "current/features/"):
			rest := strings.TrimPrefix(rel, "current/features/")
			if i := strings.Index(rest, "/"); i > 0 {
				featureDirs[rest[:i]] = true
			}
		case strings.HasPrefix(rel, "current/decisions/"):
			decisions++
		case strings.HasPrefix(rel, "current/references/"):
			references++
		}
	}
	return len(featureDirs), decisions, references
}
