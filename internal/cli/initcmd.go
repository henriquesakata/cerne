package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/hooks"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/skills"
	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/store"
)

func newInitCmd() *cobra.Command {
	var (
		name    string
		backend string
		modules []string
		dir     string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a cerne project in the current directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			targetDir := dir
			if targetDir == "" {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				targetDir = wd
			}
			return runInit(cmd, targetDir, name, backend, modules)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "project name")
	cmd.Flags().StringVar(&backend, "backend", "", "backend git URL or local path")
	cmd.Flags().StringArrayVar(&modules, "module", nil, "module to register (repeatable)")
	cmd.Flags().StringVar(&dir, "dir", "", "target project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("backend")
	return cmd
}

func runInit(cmd *cobra.Command, targetDir, name, backend string, modules []string) error {
	if _, foundDir, err := project.LoadConfig(targetDir); err == nil {
		return fmt.Errorf("project already initialized: .cernerc found in %s", foundDir)
	} else if !errors.Is(err, project.ErrNoProject) {
		return err
	}

	projectID := slug.Make(name)
	if projectID == "" {
		return fmt.Errorf("project name %q yields an empty project id", name)
	}

	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return err
	}
	repoDir := filepath.Join(cacheDir, "repo")
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err == nil {
		// Cache already holds a clone of this project's backend: refresh it.
		// SafePull, not Pull — the clone is shared with concurrent cerne
		// processes (a ui pull loop, a synthesis in flight), and a raw Pull on
		// a dirty worktree poisons it (N-0027: ref moved, files left behind).
		if err := gitback.SafePull(repoDir); err != nil {
			return fmt.Errorf("refreshing cached backend clone at %s failed (delete that directory to force a fresh clone): %w", repoDir, err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return err
		}
		if err := gitback.Clone(backend, repoDir); err != nil {
			return err
		}
	} else {
		return err
	}

	joined := true
	if _, err := store.LoadProject(repoDir); errors.Is(err, store.ErrNotFound) {
		joined = false
		if err := store.InitLayout(repoDir, store.Project{Name: name, Modules: modules}); err != nil {
			return err
		}
		if _, err := gitback.CommitAndPush(repoDir, "cerne: initialize backend layout"); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		return err
	}
	if err := project.SaveConfig(targetDir, project.Config{ProjectID: projectID, Backend: backend}); err != nil {
		return err
	}
	lock := project.Lock{Version: 1, Backend: backend, Snapshot: head}
	if err := project.SaveLock(targetDir, lock); err != nil {
		return err
	}
	docsDir := filepath.Join(targetDir, ".docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(docsDir, ".gitkeep"), nil, 0o644); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if joined {
		fmt.Fprintf(out, "Joined existing backend %s as project %q.\n", backend, projectID)
	} else {
		fmt.Fprintf(out, "Initialized project %q with backend %s.\n", projectID, backend)
	}

	// Install cerne's canonical Claude Code skills so a new project ships with
	// the agent guide. The project is already provisioned at this point, so a
	// skills failure must not fail init — warn and continue.
	if results, err := skills.Install(targetDir); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not install cerne's skills: %v\n", err)
	} else {
		printSkillResults(out, results)
	}

	// Install cerne's Claude Code hooks too (F-0139): the provenance gate and
	// the SessionStart registration reminder, so a new project is born with the
	// register-before-implementing rules injected. Same non-fatal stance.
	if results, err := hooks.Install(targetDir); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not install cerne's hooks: %v\n", err)
	} else {
		printHookResults(out, results)
	}

	// Wire docs.lock to the merge driver so concurrent branches don't conflict
	// on the snapshot pin. Non-fatal: the project is already provisioned.
	if err := ensureLockMergeDriver(targetDir); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not register the docs.lock merge driver: %v\n", err)
	}

	fmt.Fprintln(out, "Next steps:")
	fmt.Fprintln(out, "  - add .docs/ to your project's .gitignore (cerne never edits it for you)")
	fmt.Fprintln(out, "  - commit .cernerc, docs.lock and .gitattributes to your project repository")
	return nil
}
