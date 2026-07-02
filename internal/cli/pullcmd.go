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

	"gitlab.com/rockt/cerne/internal/app"
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
	ctx, err := resolvePaths(dir)
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

	var manifest map[string]string
	var head string
	var conflicts []string
	// Hold the backend lock for the whole clone-touching section: bring the
	// shared clone to the latest remote, materialize, and 3-way merge unsent
	// edits — so a concurrent push cannot reset the worktree mid-materialize.
	lockErr := gitback.WithLock(gitback.LockPath(ctx.repoDir), func() error {
		if err := gitback.Fetch(ctx.repoDir); err != nil {
			return err
		}
		theirs, err := gitback.TrackingHeadSHA(ctx.repoDir)
		if errors.Is(err, gitback.ErrNoCommits) {
			theirs = ""
		} else if err != nil {
			return err
		}
		if theirs != "" {
			if err := gitback.ResetHard(ctx.repoDir, theirs); err != nil {
				return err
			}
		}

		// Capture unsent local edits (in projDir/.docs) BEFORE materialize wipes
		// them, so the merge can restore the disjoint ones afterwards.
		prevManifest, err := project.LoadManifest(ctx.projDir)
		if err != nil {
			return err
		}
		var mine map[string][]byte
		if !force && len(prevManifest) > 0 {
			unsent, err := unpushedDocs(docsDir, prevManifest)
			if err != nil {
				return err
			}
			mine, err = readBodies(docsDir, unsent)
			if err != nil {
				return err
			}
		}

		manifest, err = materialize.Materialize(ctx.repoDir, docsDir, scope, okf)
		if err != nil {
			return err
		}
		if len(mine) > 0 {
			conflicts, err = reapplyUnsent(docsDir, prevManifest, manifest, mine)
			if err != nil {
				return err
			}
		}

		head, err = gitback.HeadSHA(ctx.repoDir)
		if errors.Is(err, gitback.ErrNoCommits) {
			head = ""
		} else if err != nil {
			return err
		}
		if err := project.SaveSessionSnapshot(ctx.projDir, head); err != nil {
			return err
		}
		// The committed docs.lock pin only moves where PinAllowed says so —
		// off the pin branch it stays byte-identical, so MRs never conflict
		// on it (F-0102).
		if app.PinAllowed(ctx.projDir) {
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
		}
		return project.SaveManifest(ctx.projDir, manifest)
	})
	if lockErr != nil {
		return fmt.Errorf("sincronizando o clone de cache em %s falhou (apague esse diretório e rode de novo para forçar um clone novo): %w", ctx.repoDir, lockErr)
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
	snap := head
	if len(snap) > 8 {
		snap = snap[:8]
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"Materializado: %d feature(s), %d decisão(ões), %d referência(s) em .docs/ (snapshot %s).\n",
		nFeatures, nDecisions, nReferences, snap)
	// Explain a known no-op instead of a mute "0 feature(s)": a scoped pull that
	// materializes nothing usually means the module's features are implemented
	// (concluded entities don't materialize — they are history).
	if scope != "" && nFeatures == 0 {
		fmt.Fprintf(cmd.OutOrStdout(),
			"Nenhuma feature do módulo %q materializou: features implementadas não materializam conteúdo (viram histórico). "+
				"Use 'cerne show <id>' para ler, ou 'cerne errata <id>' para anotar/corrigir.\n", scope)
	}
	if len(conflicts) > 0 {
		return reportPullConflicts(cmd, conflicts)
	}
	return nil
}

// readBodies reads each .docs-relative path into memory (the unsent edits to
// re-apply after materialize wipes .docs/).
func readBodies(docsDir string, rels []string) (map[string][]byte, error) {
	m := make(map[string][]byte, len(rels))
	for _, rel := range rels {
		body, err := os.ReadFile(filepath.Join(docsDir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		m[rel] = body
	}
	return m, nil
}

// reapplyUnsent merges the captured unsent edits (mine) back over the freshly
// materialized .docs/ (theirs), guided by the pre-pull manifest (base hashes).
// Disjoint edits are restored; a document changed on both sides keeps MINE in
// place (non-destructive), stashes THEIRS under .docs/.conflicts/, and is left
// flagged unsent (fresh[rel] reset to base) so it is not lost. It mutates fresh
// for conflicts and returns the conflicting paths, sorted.
func reapplyUnsent(docsDir string, prev, fresh map[string]string, mine map[string][]byte) ([]string, error) {
	var conflicts []string
	for rel, body := range mine {
		baseHash := prev[rel]
		theirsHash, theirsTracked := fresh[rel]
		mineHash := sha256Hex(body)
		switch {
		case !theirsTracked:
			// Net-new local doc (e.g. result.md before first push), or removed
			// remotely: restore mine. Absent from fresh, so it stays unsent.
			if err := writeDocsFile(docsDir, rel, body); err != nil {
				return nil, err
			}
		case theirsHash == mineHash:
			// Coincidentally identical to the remote: leave theirs in place.
		case theirsHash == baseHash:
			// Remote did not touch this doc: keep my unsent edit.
			if err := writeDocsFile(docsDir, rel, body); err != nil {
				return nil, err
			}
		default:
			// Same document changed both sides: keep MINE visible, stash THEIRS,
			// and keep it unsent (so nothing is silently discarded).
			theirs, err := os.ReadFile(filepath.Join(docsDir, filepath.FromSlash(rel)))
			if err != nil {
				return nil, err
			}
			if err := saveConflictCopy(docsDir, rel, theirs); err != nil {
				return nil, err
			}
			if err := writeDocsFile(docsDir, rel, body); err != nil {
				return nil, err
			}
			fresh[rel] = baseHash
			conflicts = append(conflicts, rel)
		}
	}
	sort.Strings(conflicts)
	return conflicts, nil
}

func writeDocsFile(docsDir, rel string, body []byte) error {
	p := filepath.Join(docsDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, body, 0o644)
}

// saveConflictCopy stashes the remote version of a conflicting doc under
// .docs/.conflicts/ (outside .docs/current/, so it is inert to push/pull walks).
func saveConflictCopy(docsDir, rel string, theirs []byte) error {
	p := filepath.Join(docsDir, ".conflicts", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, theirs, 0o644)
}

// reportPullConflicts prints the same-document conflicts a pull merged
// non-destructively (mine kept, theirs stashed) and returns a non-nil error so
// the exit code signals that resolution is needed.
func reportPullConflicts(cmd *cobra.Command, conflicts []string) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\nConflito em %d documento(s): sua versão foi mantida em .docs/; a versão remota está em .docs/.conflicts/:\n", len(conflicts))
	for _, rel := range conflicts {
		fmt.Fprintf(out, "  %s\n", rel)
	}
	fmt.Fprintln(out, "Reconcilie cada documento e rode 'cerne push' (a versão remota está em .docs/.conflicts/).")
	return fmt.Errorf("%d documento(s) em conflito no pull", len(conflicts))
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
