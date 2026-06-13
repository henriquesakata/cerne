package cli

import "github.com/spf13/cobra"

var version = "0.0.1-dev"

func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "cerne",
		Short:         "Living documentation for AI-assisted software projects",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(newInitCmd())
	root.AddCommand(newFeatureCmd())
	root.AddCommand(newRetireCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newPullCmd())
	root.AddCommand(newPushCmd())
	root.AddCommand(newImportCmd())
	root.AddCommand(newLintCmd())
	root.AddCommand(newSearchCmd())
	root.AddCommand(newUICmd())
	return root
}
