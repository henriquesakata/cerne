package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
)

// T-0006: a bug report (B-) is a feature-shaped card with a three-field spec
// and no approve step when a human asked for it.

func newBugCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "bug",
		Short: "Report corrections (B-) in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(verbs.MarkChat(newBugNewCmd(&dir)))
	return cmd
}

func newBugNewCmd(dir *string) *cobra.Command {
	var module, fixes, facet, component, file, context, steps, expected, owner, priority, onBehalf string
	var force bool
	cmd := &cobra.Command{
		Use:   "new [title]",
		Short: "Report a bug; born approved when a human asked for it",
		Long: "Creates a bug report (B-): a lightweight correction card with a three-section spec (Context, " +
			"Steps to reproduce, Expected × actual) and the regression-test bar already filled. When the origin " +
			"is a human — you at a terminal, or an agent relaying their words with --on-behalf — the card is " +
			"born approved: start it right away. A bug an agent found by itself (no --on-behalf) is born " +
			"candidate and waits for a human approve. Completing needs a real result.md; accept stays human. " +
			"--fixes links the delivered feature and inherits its facets, components and capabilities. The title argument may be omitted when the body (--file or stdin) opens with a `# ` heading: that heading becomes the title.",
		Example: "  cerne bug new \"<what breaks>\" --context \"<what happens, where>\" --steps \"<numbered steps>\" --expected \"<should happen × happened>\" --on-behalf \"<the human's words>\"\n" +
			"  cerne bug new \"<what breaks>\" --file report.md --fixes F-NNNN\n" +
			"  cerne feature start B-NNNN     # no plan gate, no approve when the report is human",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBugNew(cmd, *dir, args, module, fixes, facet, component, file, context, steps, expected, force, owner, priority, onBehalf)
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "module the bug belongs to")
	cmd.Flags().StringVar(&fixes, "fixes", "", "delivered feature whose defect this corrects (F-0321); its tags are inherited")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (CSV; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (CSV; subsystem/tool, e.g. gitback,materialize)")
	cmd.Flags().StringVar(&file, "file", "", "file with the full report (defaults to piped stdin)")
	cmd.Flags().StringVar(&context, "context", "", "what happens and where (1-3 sentences), seeded into the Context section")
	cmd.Flags().StringVar(&steps, "steps", "", "numbered steps to the defect, seeded into the Steps to reproduce section")
	cmd.Flags().StringVar(&expected, "expected", "", "what should happen × what happened, seeded into the Expected × actual section")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard (F-0147) for a consciously distinct problem, and accept a component near an existing one (E-0005; check 'cerne tags' first)")
	cmd.Flags().StringVar(&owner, "owner", "", "responsible handle (free text, F-0204)")
	cmd.Flags().StringVar(&priority, "priority", "", "priority (closed set: p0,p1,p2,p3, F-0204)")
	cmd.Flags().StringVar(&onBehalf, "on-behalf", "", onBehalfHelp)
	addOriginFlag(cmd)
	return cmd
}

func runBugNew(cmd *cobra.Command, dir string, args []string, module, fixes, facet, component, file, context, steps, expected string, force bool, owner, priority, onBehalf string) error {
	if cmd.Flags().Changed("on-behalf") && strings.TrimSpace(onBehalf) == "" {
		return app.ErrOnBehalfEmpty
	}
	if err := checkOriginChoice(cmd, onBehalf); err != nil {
		return err
	}
	body, err := readOptionalBody(cmd, file)
	if err != nil {
		return err
	}
	title, err := newTitle(args, body)
	if err != nil {
		return err
	}
	hasBody, hasContext := strings.TrimSpace(body) != "", strings.TrimSpace(context) != ""
	if hasBody && hasContext {
		return fmt.Errorf("--file/stdin and --context are mutually exclusive: the full report already carries the context")
	}
	if hasBody && (strings.TrimSpace(steps) != "" || strings.TrimSpace(expected) != "") {
		return fmt.Errorf("--file/stdin and --steps/--expected are mutually exclusive: the full report already carries them")
	}
	if !hasBody && !hasContext {
		return errFeatureContextRequired
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	lang := store.DocsLanguage(ctx.repoDir)
	specBody := body
	if hasContext {
		specBody = spectmpl.BugBodyIn(lang, context, steps, expected)
	} else if _, err := doccheck.SpecBarIn([]byte(body), lang); err != nil {
		// A born-approved report never meets the approve gate: give it the
		// regression-test exemption unless the file brought a valid bar.
		specBody = strings.TrimRight(body, "\n") + "\n\n" + spectmpl.BugBarIn(lang) + "\n"
	}
	notice := componentNotice(cmd, ctx.repoDir, component)
	f, err := app.CreateBugWithSpec(ctx.repoDir, ctx.projDir, title, module, strings.TrimSpace(fixes),
		splitInforms(facet), splitInforms(component), specBody, force, owner, priority, onBehalf, app.DocAuthor{})
	if err != nil {
		return err
	}
	notice()
	fmt.Fprintf(cmd.OutOrStdout(), "%s created (%s) with its report as the spec. Run 'cerne pull' to materialize it locally.\n", f.ID, f.State)
	if f.State == domain.StateApproved {
		fmt.Fprintf(cmd.OutOrStdout(), "Next step: the human report is the approval — run 'cerne feature start %s' now, no plan needed.\n", f.ID)
		return nil
	}
	imps := printApproveReadiness(cmd.OutOrStdout(), ctx.repoDir, ctx.projDir, f.ID)
	printNewCardNextStep(cmd.OutOrStdout(), ctx, f, hasContext, imps)
	return nil
}
