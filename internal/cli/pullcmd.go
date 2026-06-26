package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/index"
	"gitlab.com/rockt/cerne/internal/materialize"
	"gitlab.com/rockt/cerne/internal/project"
)

func newPullCmd() *cobra.Command {
	var dir, scope string
	var okf, force bool
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Materialize the current project state into .docs/",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPull(cmd, dir, scope, okf, force)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().StringVar(&scope, "scope", "", "materialize only features of this module")
	cmd.Flags().BoolVar(&okf, "okf", false, "emitir frontmatter compatível com o Open Knowledge Format em .docs/")
	cmd.Flags().BoolVar(&force, "force", false, "descartar edições locais não-enviadas em .docs/ (sobrescreve o guard)")
	return cmd
}

func runPull(cmd *cobra.Command, dir, scope string, okfFlag, force bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if scope == "" {
		scope = ctx.cfg.DefaultScope
	}
	// .cernerc sets the default; an explicit --okf flag overrides it either way.
	okf := ctx.cfg.OKF
	if cmd.Flags().Changed("okf") {
		okf = okfFlag
	}
	docsDir := filepath.Join(ctx.projDir, ".docs")
	// pull wipes .docs/ via RemoveAll; refuse when there are unpushed local
	// edits (unless --force) so authored content is never lost silently.
	if !force {
		prev, err := project.LoadManifest(ctx.projDir)
		if err != nil {
			return err
		}
		if len(prev) > 0 {
			unpushed, err := unpushedDocs(docsDir, prev)
			if err != nil {
				return err
			}
			if len(unpushed) > 0 {
				return fmt.Errorf(
					"pull descartaria %d edição(ões) não-enviada(s) em .docs/: %s — rode 'cerne push' ou use --force",
					len(unpushed), strings.Join(unpushed, ", "))
			}
		}
	}
	manifest, err := materialize.Materialize(ctx.repoDir, docsDir, scope, okf)
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
	// Explain a known no-op instead of a mute "0 feature(s)": a scoped pull that
	// materializes nothing usually means the module's features are implemented
	// (concluded entities don't materialize — they are history).
	if scope != "" && nFeatures == 0 {
		fmt.Fprintf(cmd.OutOrStdout(),
			"Nenhuma feature do módulo %q materializou: features implementadas não materializam conteúdo (viram histórico). "+
				"Use 'cerne show <id>' para ler, ou 'cerne errata <id>' para anotar/corrigir.\n", scope)
	}
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

// unpushedDocs returns the .docs/current/**.md paths whose content diverges from
// the manifest (edited or net-new) — local authoring a pull's RemoveAll would
// discard. Rels are slash-separated and sorted. Reuses sha256Hex (pushcmd.go).
func unpushedDocs(docsDir string, manifest map[string]string) ([]string, error) {
	currentDir := filepath.Join(docsDir, "current")
	if _, err := os.Stat(currentDir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var changed []string
	err := filepath.WalkDir(currentDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		relOS, err := filepath.Rel(docsDir, p)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(relOS)
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if want, ok := manifest[rel]; !ok || sha256Hex(body) != want {
			changed = append(changed, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(changed)
	return changed, nil
}
