package cli

import "github.com/spf13/cobra"

// version is the build version reported by `cerne --version`. It defaults to
// "dev" for local builds and is stamped at release time via:
//
//	-ldflags "-X gitlab.com/rockt/cerne/internal/cli.version=<tag>"
var version = "dev"

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
	root.AddCommand(newDecisionCmd())
	root.AddCommand(newRunbookCmd())
	root.AddCommand(newNoteCmd())
	root.AddCommand(newReferenceCmd())
	root.AddCommand(newRetireCmd())
	root.AddCommand(newForgetAliasCmd())
	root.AddCommand(newShowCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newNextCmd())
	root.AddCommand(newBriefCmd())
	root.AddCommand(newPullCmd())
	root.AddCommand(newPushCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newImportCmd())
	root.AddCommand(newClassifyCmd())
	root.AddCommand(newLintCmd())
	root.AddCommand(newDriftCmd())
	root.AddCommand(newSearchCmd())
	root.AddCommand(newGraphCmd())
	root.AddCommand(newCompileCmd())
	root.AddCommand(newAskCmd())
	root.AddCommand(newSpecCmd())
	root.AddCommand(newPlanCmd())
	root.AddCommand(newResultCmd())
	root.AddCommand(newUICmd())
	root.AddCommand(newServeCmd())
	root.AddCommand(newSkillCmd())
	root.AddCommand(newHookCmd())
	root.AddCommand(newLockCmd())
	return root
}
