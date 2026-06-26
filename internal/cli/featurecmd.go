package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
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
	cmd.AddCommand(newFeatureReparentCmd(&dir))
	cmd.AddCommand(newTransitionCmds(&dir)...)
	cmd.AddCommand(newErrataCmd(&dir))
	return cmd
}

func newFeatureNewCmd(dir *string) *cobra.Command {
	var module string
	var parent string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a feature in candidate state",
		Example: "  # registra a candidata (no quadro) e materializa o slot da spec\n" +
			"  cerne feature new \"<título>\" --module <m> [--parent F-NNNN]\n" +
			"  cerne pull\n" + lifecycleSeq,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureNew(cmd, *dir, args[0], module, parent)
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "module the feature belongs to")
	cmd.Flags().StringVar(&parent, "parent", "", "parent feature ID (the epic this one decomposes)")
	return cmd
}

func runFeatureNew(cmd *cobra.Command, dir, title, module, parent string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	f, err := app.CreateFeature(ctx.repoDir, ctx.projDir, title, module, parent)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s criada (candidate). Próximo passo: escreva a spec e rode 'cerne pull'.\n", f.ID)
	return nil
}
