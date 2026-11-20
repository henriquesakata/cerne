package cli

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/llm"
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
		Args:          cobra.ArbitraryArgs, // non-nil: cobra's own legacy check would answer first, without the guide hint
		RunE:          rootRunE,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := refuseAgentLifecycle(cmd); err != nil {
				return err
			}
			startWriteNotice()
			app.RemintNotice = func(oldID, newID string) { // F-0356
				fmt.Fprintf(cmd.ErrOrStderr(), "cerne: %s was taken upstream; registered as %s\n", oldID, newID)
			}
			app.IndexNotice = func(err error) { // F-0394
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: search index not refreshed: %v; run cerne pull\n", err)
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
	root.AddGroup(
		&cobra.Group{ID: "start", Title: "Start here:"},
		&cobra.Group{ID: "read", Title: "Read the state:"},
		&cobra.Group{ID: "author", Title: "Author and move the work:"},
		&cobra.Group{ID: "sync", Title: "Sync with the backend:"},
		&cobra.Group{ID: "agents", Title: "Agents and coordination:"},
		&cobra.Group{ID: "admin", Title: "Administration and housekeeping:"},
	)
	// "section", not "group": group already means a command with subcommands in this package.
	addIn := func(section string, c *cobra.Command) {
		c.GroupID = section
		root.AddCommand(c)
	}
	addIn("start", verbs.MarkLocal(newInitCmd()))
	addIn("start", verbs.MarkLocal(newDemoCmd()))
	addIn("start", verbs.MarkLocal(newUpdateCmd()))
	addIn("author", newFeatureCmd())
	addIn("author", newEpicCmd())
	addIn("author", newTaskCmd())
	addIn("author", newBugCmd())
	addIn("author", newDecisionCmd())
	addIn("author", newRunbookCmd())
	addIn("read", verbs.MarkLocal(newAcceptCmd()))
	addIn("author", newNoteCmd())
	addIn("agents", verbs.MarkLocal(newWorkCmd()))
	addIn("agents", verbs.MarkLocal(newHandoffCmd()))
	addIn("author", newPendingCmd())
	addIn("author", newIdeaCmd())
	addIn("author", newCapabilityCmd())
	addIn("author", newReferenceCmd())
	addIn("admin", newRetireCmd())
	addIn("admin", verbs.MarkChat(newForgetAliasCmd()))
	addIn("read", verbs.MarkChat(newShowCmd()))
	addIn("read", verbs.MarkChat(newStatusCmd()))
	addIn("read", verbs.MarkChat(newNextCmd()))
	addIn("read", verbs.MarkChat(newBriefCmd()))
	addIn("start", verbs.MarkChat(newGuideCmd()))
	addIn("read", verbs.MarkChat(newSavingsCmd()))
	addIn("read", verbs.MarkChat(newStatsCmd()))
	addIn("read", verbs.MarkLocal(newCostCmd()))
	addIn("sync", verbs.MarkLocal(newPullCmd()))
	addIn("sync", verbs.MarkLocal(newPushCmd()))
	addIn("sync", verbs.MarkLocal(newSyncCmd()))
	addIn("start", verbs.MarkLifecycle(newImportCmd())) // mints entities in any state, past every gate
	addIn("author", verbs.MarkChat(newClassifyCmd()))
	addIn("read", verbs.MarkChat(newLintCmd()))
	addIn("author", newClarifyCmd())
	addIn("sync", verbs.MarkLocal(newDriftCmd()))
	addIn("sync", newMirrorCmd())
	addIn("read", verbs.MarkChat(newSearchCmd()))
	addIn("read", verbs.MarkChat(newGraphCmd()))
	addIn("sync", verbs.MarkLocal(newCompileCmd()))
	addIn("read", verbs.MarkLocal(newAskCmd()))
	addIn("author", verbs.MarkLocal(newSpecCmd()))
	addIn("author", verbs.MarkLocal(newPlanCmd()))
	addIn("author", verbs.MarkLocal(newResultCmd()))
	addIn("author", verbs.MarkLocal(newGauntletCmd()))
	addIn("start", verbs.MarkLocal(newUICmd()))
	addIn("admin", verbs.MarkLocal(newServeCmd()))
	addIn("agents", verbs.MarkLocal(newMCPCmd()))
	addIn("admin", verbs.MarkLocal(newDoctorCmd()))
	addIn("agents", verbs.MarkLocal(newSkillCmd()))
	addIn("agents", verbs.MarkLocal(newHookCmd()))
	addIn("agents", verbs.MarkLocal(newHarnessCmd()))
	addIn("admin", verbs.MarkLocal(newModelsCmd()))
	addIn("admin", verbs.MarkLocal(newSettingsCmd()))
	addIn("agents", verbs.MarkLocal(newGuardCmd()))
	addIn("sync", verbs.MarkLocal(newLockCmd()))
	addIn("agents", verbs.MarkLocal(newRoomCmd()))
	addIn("agents", verbs.MarkLocal(newMsgCmd()))
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

// guideHint is the last line of every unknown-command refusal (F-0398).
const guideHint = "\n" + `or ask: cerne guide "<what you want to do>"`

// rootRunE keeps bare `cerne` printing help and gives an unknown first word
// cobra's own message and suggestions, plus the guide hint (F-0398).
func rootRunE(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	if sug := cmd.SuggestionsFor(args[0]); len(sug) > 0 {
		msg += "\n\nDid you mean this?\n\t" + strings.Join(sug, "\n\t")
	}
	return fmt.Errorf("%s%s", msg, guideHint)
}

func groupRunE(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	if args[0] == "help" { // root help takes the group path: `feature help approve` -> `help feature approve`
		path := strings.TrimPrefix(cmd.CommandPath(), "cerne ")
		return fmt.Errorf("%s\n\nDid you mean: cerne help %s?%s", msg, strings.Join(append([]string{path}, args[1:]...), " "), guideHint)
	}
	for _, rc := range cmd.Root().Commands() {
		if (rc.Name() == args[0] || rc.HasAlias(args[0])) && rc != cmd {
			return fmt.Errorf("%s\n\nDid you mean: cerne %s?%s", msg, strings.Join(args, " "), guideHint)
		}
	}
	cmd.SuggestionsMinimumDistance = 2 // cobra only defaults this on the root
	if sug := cmd.SuggestionsFor(args[0]); len(sug) > 0 {
		msg += "\n\nDid you mean this?\n\t" + strings.Join(sug, "\n\t")
	}
	return fmt.Errorf("%s%s", msg, guideHint)
}

// refuseAgentLifecycle is the binary's own lifecycle refusal for the Backlog
// agent (B-0040): agentRun stamps llm.DenyLifecycleEnv on every command the
// agent runs, and this check runs on the command cobra resolved, so a flag
// before the verb, or any other spelling the harness prefix rules miss, is
// refused all the same. Presence, not value: an empty `CERNE_DENY_LIFECYCLE=`
// assignment still refuses. It holds only while the root's PersistentPreRunE
// is the one cobra runs, which TestLifecycleRefusedUnderAgentHarnessStamp pins.
func refuseAgentLifecycle(cmd *cobra.Command) error {
	if _, stamped := os.LookupEnv(llm.DenyLifecycleEnv); stamped && verbs.IsLifecycle(cmd) {
		return fmt.Errorf("%w: %s", verbs.ErrLifecycleDenied, cmd.CommandPath())
	}
	return nil
}
