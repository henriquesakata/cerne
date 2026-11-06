package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/telemetry"
	"gitlab.com/rockt/cerne/internal/verbs"
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
		// The docs.lock pin is deferred+coalesced (F-0153); a one-shot CLI
		// process must land it before exiting.
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			app.FlushAutocommits()
		},
	}
	root.PersistentFlags().BoolVar(&verbose, "verbose", false, "log operation timings (phases, retries) to stderr")
	root.SetVersionTemplate("cerne {{.Version}}\n")
	root.AddCommand(verbs.MarkLocal(newInitCmd()))
	root.AddCommand(newFeatureCmd())
	root.AddCommand(newEpicCmd())
	root.AddCommand(newTaskCmd())
	root.AddCommand(newDecisionCmd())
	root.AddCommand(newRunbookCmd())
	root.AddCommand(newNoteCmd())
	root.AddCommand(verbs.MarkLocal(newWorkCmd()))
	root.AddCommand(newPendingCmd())
	root.AddCommand(newCapabilityCmd())
	root.AddCommand(newReferenceCmd())
	root.AddCommand(newRetireCmd())
	root.AddCommand(verbs.MarkChat(newForgetAliasCmd()))
	root.AddCommand(verbs.MarkChat(newShowCmd()))
	root.AddCommand(verbs.MarkChat(newStatusCmd()))
	root.AddCommand(verbs.MarkChat(newNextCmd()))
	root.AddCommand(verbs.MarkChat(newBriefCmd()))
	root.AddCommand(verbs.MarkChat(newSavingsCmd()))
	root.AddCommand(verbs.MarkChat(newStatsCmd()))
	root.AddCommand(verbs.MarkLocal(newCostCmd()))
	root.AddCommand(verbs.MarkLocal(newPullCmd()))
	root.AddCommand(verbs.MarkLocal(newPushCmd()))
	root.AddCommand(verbs.MarkLocal(newSyncCmd()))
	root.AddCommand(verbs.MarkLifecycle(newImportCmd())) // mints entities in any state, past every gate
	root.AddCommand(verbs.MarkChat(newClassifyCmd()))
	root.AddCommand(verbs.MarkChat(newLintCmd()))
	root.AddCommand(newClarifyCmd())
	root.AddCommand(verbs.MarkLocal(newDriftCmd()))
	root.AddCommand(verbs.MarkChat(newSearchCmd()))
	root.AddCommand(verbs.MarkChat(newGraphCmd()))
	root.AddCommand(verbs.MarkLocal(newCompileCmd()))
	root.AddCommand(verbs.MarkLocal(newAskCmd()))
	root.AddCommand(verbs.MarkLocal(newSpecCmd()))
	root.AddCommand(verbs.MarkLocal(newPlanCmd()))
	root.AddCommand(verbs.MarkLocal(newResultCmd()))
	root.AddCommand(verbs.MarkLocal(newUICmd()))
	root.AddCommand(verbs.MarkLocal(newServeCmd()))
	root.AddCommand(verbs.MarkLocal(newDoctorCmd()))
	root.AddCommand(verbs.MarkLocal(newSkillCmd()))
	root.AddCommand(verbs.MarkLocal(newHookCmd()))
	root.AddCommand(verbs.MarkLocal(newHarnessCmd()))
	root.AddCommand(verbs.MarkLocal(newModelsCmd()))
	root.AddCommand(verbs.MarkLocal(newSettingsCmd()))
	root.AddCommand(verbs.MarkLocal(newGuardCmd()))
	root.AddCommand(verbs.MarkLocal(newLockCmd()))
	root.AddCommand(verbs.MarkLocal(newRoomCmd()))
	root.AddCommand(verbs.MarkLocal(newMsgCmd()))
	return root
}
