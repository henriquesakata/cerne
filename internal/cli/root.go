package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/telemetry"
	"gitlab.com/rockt/cerne/internal/version"
)

func Root() *cobra.Command {
	var verbose bool
	root := &cobra.Command{
		Use:           "cerne",
		Short:         "Living documentation for AI-assisted software projects",
		Version:       version.Short(),
		SilenceUsage:  true,
		SilenceErrors: false,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if verbose {
				// Text to stderr: op timings become visible without ever
				// polluting the command's stdout (F-0149).
				telemetry.SetupLogger(os.Stderr, false, slog.LevelDebug)
			}
		},
	}
	root.PersistentFlags().BoolVar(&verbose, "verbose", false, "log operation timings (phases, retries) to stderr")
	root.SetVersionTemplate("cerne {{.Version}}\n")
	root.AddCommand(newInitCmd())
	root.AddCommand(newFeatureCmd())
	root.AddCommand(newDecisionCmd())
	root.AddCommand(newRunbookCmd())
	root.AddCommand(newNoteCmd())
	root.AddCommand(newWorkCmd())
	root.AddCommand(newReferenceCmd())
	root.AddCommand(newRetireCmd())
	root.AddCommand(newForgetAliasCmd())
	root.AddCommand(newShowCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newNextCmd())
	root.AddCommand(newBriefCmd())
	root.AddCommand(newStatsCmd())
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
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newSkillCmd())
	root.AddCommand(newHookCmd())
	root.AddCommand(newLockCmd())
	return root
}
