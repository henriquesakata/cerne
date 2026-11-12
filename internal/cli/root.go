package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/telemetry"
	"github.com/henriquesakata/cerne/internal/verbs"
	"github.com/henriquesakata/cerne/internal/version"
)

func Root() *cobra.Command {
	var verbose bool
	root := &cobra.Command{
		Use:           "cerne",
		Short:         "Living documentation for AI-assisted software projects",
		Version:       version.Short(),
		SilenceUsage:  true,
		SilenceErrors: false,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			startWriteNotice()
			app.RemintNotice = func(oldID, newID string) { // F-0356
				fmt.Fprintf(cmd.ErrOrStderr(), "cerne: %s was taken upstream; registered as %s\n", oldID, newID)
			}
			if verbose {
				// Text to stderr: op timings become visible without ever
				// polluting the command's stdout (F-0149).
				telemetry.SetupLogger(os.Stderr, false, slog.LevelDebug)
			}
			if err := refuseProjectlessDir(cmd); err != nil {
				if cmd.Annotations[finalLineAnnotation] != "" { // push/sync: the refusal ends in the FALHA line (F-0876)
					return failWithLine(cmd, err)
				}
				return err
			}
			return nil
		},
		// The docs.lock pin is deferred+coalesced (F-0153); a one-shot CLI
		// process must land it before exiting.
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			app.FlushAutocommits()
			noteWrite(cmd.ErrOrStderr())
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
	root.AddCommand(verbs.MarkLocal(newAcceptCmd()))
	root.AddCommand(newNoteCmd())
	root.AddCommand(verbs.MarkLocal(newWorkCmd()))
	root.AddCommand(newPendingCmd())
	root.AddCommand(newIdeaCmd())
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
	root.InitDefaultCompletionCmd() // cobra adds it only at Execute, after the guard would have run
	for _, c := range root.Commands() {
		if c.Name() == "completion" {
			verbs.MarkLocal(c) // shell scaffolding: the human's terminal only
		}
	}
	guardGroups(root)
	return root
}

// guardGroups makes every group (subcommands, no Run of its own) fail on an
// unknown first argument instead of printing help with exit 0 (F-0344): cobra
// only checks that at the root, and a non-runnable group short-circuits to help
// before argument validation.
func guardGroups(c *cobra.Command) {
	for _, sub := range c.Commands() {
		if sub.HasSubCommands() && !sub.Runnable() {
			sub.RunE = groupRunE
			verbs.MarkGroup(sub)
		}
		guardGroups(sub)
	}
}

func groupRunE(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	if args[0] == "help" { // root help takes the group path: `feature help approve` -> `help feature approve`
		path := strings.TrimPrefix(cmd.CommandPath(), "cerne ")
		return fmt.Errorf("%s\n\nDid you mean: cerne help %s?", msg, strings.Join(append([]string{path}, args[1:]...), " "))
	}
	for _, rc := range cmd.Root().Commands() {
		if (rc.Name() == args[0] || rc.HasAlias(args[0])) && rc != cmd {
			return fmt.Errorf("%s\n\nDid you mean: cerne %s?", msg, strings.Join(args, " "))
		}
	}
	cmd.SuggestionsMinimumDistance = 2 // cobra only defaults this on the root
	if sug := cmd.SuggestionsFor(args[0]); len(sug) > 0 {
		msg += "\n\nDid you mean this?\n\t" + strings.Join(sug, "\n\t")
	}
	return fmt.Errorf("%s", msg)
}
