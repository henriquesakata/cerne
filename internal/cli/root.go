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
	return root
}
