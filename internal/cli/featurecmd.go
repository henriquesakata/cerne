package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

func newFeatureCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "feature",
		Short: "Create and manage features in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newFeatureNewCmd(&dir))
	cmd.AddCommand(newTransitionCmds(&dir)...)
	return cmd
}

func newFeatureNewCmd(dir *string) *cobra.Command {
	var module string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a feature in candidate state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureNew(cmd, *dir, args[0], module)
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "module the feature belongs to")
	return cmd
}

func runFeatureNew(cmd *cobra.Command, dir, title, module string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("o título da feature não pode ser vazio")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	proj, err := store.LoadProject(ctx.repoDir)
	if err != nil {
		return err
	}
	if err := validateModule(module, proj.Modules); err != nil {
		return err
	}
	id, err := store.NextID(ctx.repoDir, "F")
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	f := store.Feature{
		ID:        id,
		Title:     title,
		Module:    module,
		State:     domain.StateCandidate,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.SaveFeature(ctx.repoDir, f); err != nil {
		return err
	}
	if err := writeSpecStub(ctx.repoDir, id, title); err != nil {
		return err
	}
	ev := domain.NewEvent(id, "feature.created", map[string]string{"title": title, "module": module}, eventActor())
	if err := store.AppendEvent(ctx.repoDir, ev); err != nil {
		return err
	}
	sha, err := gitback.CommitAndPush(ctx.repoDir, fmt.Sprintf("cerne: create %s (%s)", id, title))
	if err != nil {
		return err
	}
	if err := updateLockSnapshot(ctx.projDir, sha); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s criada (candidate). Próximo passo: escreva a spec e rode 'cerne pull'.\n", id)
	return nil
}

// validateModule enforces the project module rules: with 2+ declared modules
// --module is required; whenever the project declares modules, a non-empty
// --module must be one of them.
func validateModule(module string, modules []string) error {
	if len(modules) >= 2 && module == "" {
		return fmt.Errorf("--module é obrigatório neste projeto (módulos: %s)", strings.Join(modules, ", "))
	}
	if module != "" && len(modules) > 0 && !slices.Contains(modules, module) {
		return fmt.Errorf("módulo %q não existe no projeto (módulos: %s)", module, strings.Join(modules, ", "))
	}
	return nil
}

// featureDocPath returns the backend path of a feature lifecycle document.
func featureDocPath(repoDir, id, filename string) string {
	return filepath.Join(repoDir, "content", "features", id, filename)
}

// writeSpecStub creates content/features/<id>/spec.md with the placeholder
// body the approve gate recognizes as "spec still pending".
func writeSpecStub(repoDir, id, title string) error {
	path := featureDocPath(repoDir, id, "spec.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf("---\ncerne_id: %s\n---\n\n# %s\n\n> Spec pendente — escreva o problema, objetivo e critérios de aceite.\n", id, title)
	return os.WriteFile(path, []byte(content), 0o644)
}
