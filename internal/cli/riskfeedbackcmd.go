package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/verbs"
)

// newFeatureFeedbackCmd is the owner's answer to a decision of the risk gate
// (F-0518): the gate let a delivery pass that needed a person, or held one
// that did not. Never an agent's: it teaches the gate.
func newFeatureFeedbackCmd(dir *string) *cobra.Command {
	var shouldHaveAsked, notNeeded bool
	var reason string
	cmd := &cobra.Command{
		Use:   "feedback <id>",
		Short: "The owner's answer to a risk-gate decision: it should have asked, or it was not needed (F-0518)",
		Long: "Under [gates] accept = \"risk\", records the owner's disagreement with the gate's decision on a card: " +
			"--should-have-asked on a card it accepted as low risk, --not-needed on one it held for a person. In " +
			"calibration, where the gate holds every delivery, they answer \"did this delivery need you?\": it did, " +
			"it did not. A later answer replaces the earlier one; both stay in the log. The answers feed the gate's " +
			"proposals to change its lists, which only the owner applies. The owner's act: an agent is refused.",
		Example: "  cerne feature feedback F-0001 --should-have-asked --reason \"it changed the billing rules\"\n" +
			"  cerne feature feedback F-0002 --not-needed",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if shouldHaveAsked == notNeeded {
				return fmt.Errorf("pass exactly one answer: --should-have-asked or --not-needed")
			}
			if err := app.CheckOwnerAct("answering the risk gate"); err != nil {
				return err
			}
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			answer := app.FeedbackNotNeeded
			if shouldHaveAsked {
				answer = app.FeedbackShouldHaveAsked
			}
			id := domain.CanonicalID(args[0])
			d, err := app.RecordRiskFeedback(ctx.repoDir, ctx.projDir, id, app.RiskFeedback{Answer: answer, Reason: reason})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: answer recorded — %s, on a delivery %s\n", id, strings.ReplaceAll(answer, "_", " "), d.Says())
			return nil
		},
	}
	cmd.Flags().BoolVar(&shouldHaveAsked, "should-have-asked", false, "the delivery needed a person (in calibration: it did)")
	cmd.Flags().BoolVar(&notNeeded, "not-needed", false, "the delivery did not need a person (in calibration: it did not)")
	cmd.Flags().StringVar(&reason, "reason", "", "optional: why, in your words (kept for you to read; the gate does not learn from it)")
	return verbs.MarkLifecycle(cmd)
}
