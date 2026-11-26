package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
)

// F-0203: epics (E-) and tasks (T-) reuse the feature entity — same lifecycle,
// store and documents. `epic new` mints a declared epic (full lifecycle, born
// candidate); `task new` mints an execution unit of a committed PRD, born
// approved (the human gate was paid on the parent) with a light start gate.

func newEpicCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "epic",
		Short: "Create declared epics (E-) in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(verbs.MarkChat(newEpicNewCmd(&dir)))
	return cmd
}

func newEpicNewCmd(dir *string) *cobra.Command {
	var module, parent, facet, component, file, context, owner, priority, onBehalf, capability string
	var force bool
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create an epic in candidate state",
		Long: "Creates a declared epic (E-): a feature with its own id sequence and the full lifecycle, " +
			"for a PRD known to decompose. Epic behavior stays structural (a feature with children acts as an " +
			"epic regardless of prefix); E- only signals the intent at birth. The same creation gate applies: " +
			"pass --context (or the full spec via --file/stdin).",
		Example: "  cerne epic new \"<title>\" --context \"<1-3 sentences on why>\"\n" +
			"  cerne task new \"<step>\" --parent E-NNNN --context \"...\"   # decompose it\n" +
			"  cerne pull\n" + lifecycleSeq,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureLikeNew(cmd, *dir, args, module, parent, facet, component, file, context, force, owner, priority, onBehalf, capability, "E", "")
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "module the epic belongs to")
	cmd.Flags().StringVar(&parent, "parent", "", "parent feature ID (an epic can nest under another)")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (CSV; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (CSV; subsystem/tool, e.g. gitback,materialize)")
	cmd.Flags().StringVar(&file, "file", "", "file with the full spec body (defaults to piped stdin)")
	cmd.Flags().StringVar(&context, "context", "", "creation context (1-3 sentences) seeded into the spec's Problem section")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard (F-0147) for a consciously distinct problem, and accept a component near an existing one (E-0005; check 'cerne tags' first)")
	cmd.Flags().StringVar(&owner, "owner", "", "responsible handle (free text, F-0204)")
	cmd.Flags().StringVar(&priority, "priority", "", "priority (closed set: p0,p1,p2,p3, F-0204)")
	cmd.Flags().StringVar(&onBehalf, "on-behalf", "", onBehalfHelp)
	cmd.Flags().StringVar(&capability, "capability", "", "capability nodes to link at birth (CSV of C- ids; validated before anything is written, one commit)")
	return cmd
}

func newTaskCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Create tasks (T-) in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(verbs.MarkChat(newTaskNewCmd(&dir)))
	return cmd
}

func newTaskNewCmd(dir *string) *cobra.Command {
	var module, parent, facet, component, file, context, owner, priority, onBehalf, capability string
	var force bool
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a task, born approved under a committed PRD",
		Long: "Creates a task (T-): an execution unit of a PRD already past its human gate. --parent is " +
			"mandatory and must name a committed feature or epic (approved or beyond); the task is born " +
			"approved and its start needs no plan of its own — the why lives in the parent PRD. Completing " +
			"still requires a real result.md.",
		Example: "  cerne task new \"<step>\" --parent F-NNNN --context \"<what this step does>\"\n" +
			"  cerne feature start T-NNNN     # no plan gate\n" +
			"  cerne feature complete T-NNNN  # result.md still required",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureLikeNew(cmd, *dir, args, module, parent, facet, component, file, context, force, owner, priority, onBehalf, capability, "T", "")
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "module the task belongs to")
	cmd.Flags().StringVar(&parent, "parent", "", "the committed F-/E- this task decomposes (required)")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (CSV; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (CSV; subsystem/tool, e.g. gitback,materialize)")
	cmd.Flags().StringVar(&file, "file", "", "file with the full spec body (defaults to piped stdin)")
	cmd.Flags().StringVar(&context, "context", "", "creation context (1-3 sentences) seeded into the spec's Problem section")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard (F-0147) for a consciously distinct problem, and accept a component near an existing one (E-0005; check 'cerne tags' first)")
	cmd.Flags().StringVar(&owner, "owner", "", "responsible handle (free text, F-0204)")
	cmd.Flags().StringVar(&priority, "priority", "", "priority (closed set: p0,p1,p2,p3, F-0204)")
	cmd.Flags().StringVar(&onBehalf, "on-behalf", "", onBehalfHelp)
	cmd.Flags().StringVar(&capability, "capability", "", "capability nodes to link at birth (CSV of C- ids; validated before anything is written, one commit)")
	return cmd
}

// runFeatureLikeNew is runFeatureNew generalized over the creation core
// (F-0203): the same context gate, spec seeding and messages for F-/E-/T-.
func runFeatureLikeNew(cmd *cobra.Command, dir string, args []string, module, parent, facet, component, file, context string, force bool, owner, priority, onBehalf, capability, prefix, fixes string) error {
	// An explicit blank --on-behalf must not read as "flag not passed" (T-0005).
	if cmd.Flags().Changed("on-behalf") && strings.TrimSpace(onBehalf) == "" {
		return app.ErrOnBehalfEmpty
	}
	body, err := readOptionalBody(cmd, file)
	if err != nil {
		return err
	}
	title, err := newTitle(args, body)
	if err != nil {
		return err
	}
	hasBody := strings.TrimSpace(body) != ""
	hasContext := strings.TrimSpace(context) != ""
	if hasBody && hasContext {
		return fmt.Errorf("--file/stdin and --context are mutually exclusive: the full spec already carries the context")
	}
	if !hasBody && !hasContext {
		return errFeatureContextRequired
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	specBody := body
	if hasContext {
		specBody = spectmpl.StubWithContextIn(store.DocsLanguage(ctx.repoDir), title, context)
	}
	notice := componentNotice(cmd, ctx.repoDir, component)
	f, err := app.CreateLike(ctx.repoDir, ctx.projDir, prefix, title, module, parent, fixes,
		splitInforms(facet), splitInforms(component), splitInforms(capability), specBody, force, owner, priority, onBehalf)
	if err != nil {
		return err
	}
	notice()
	if f.Parent != "" && app.EpicPlanStub(ctx.repoDir, f.Parent) {
		fmt.Fprintf(cmd.OutOrStdout(), "epic plan scaffolded — fill it in before approve (N-0031)\n")
	}
	if hasBody {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s created (%s) with spec — already synced. Run 'cerne pull' to materialize it locally.\n", f.ID, f.State)
		printBirthNotes(cmd.OutOrStdout(), ctx, f)
		imps := printApproveReadiness(cmd.OutOrStdout(), ctx.repoDir, ctx.projDir, f.ID)
		printNewCardNextStep(cmd.OutOrStdout(), ctx, f, false, imps)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s created (%s) with its context seeded into the spec.\n", f.ID, f.State)
	printBirthNotes(cmd.OutOrStdout(), ctx, f)
	// The seeded spec is already a written one for lint and --check (F-0308).
	imps := printApproveReadiness(cmd.OutOrStdout(), ctx.repoDir, ctx.projDir, f.ID)
	printNewCardNextStep(cmd.OutOrStdout(), ctx, f, true, imps)
	return nil
}

// printBirthNotes is what a card's birth says about its parent's gates
// (T-0007): a candidate born under a parent already past approval hears that
// 'cerne task new' is the path born approved (crit. 8 — a hint, nothing
// changes); a task gets its level advice (crit. 6).
func printBirthNotes(w io.Writer, ctx projCtx, f store.Feature) {
	if f.State == domain.StateCandidate && f.Parent != "" {
		if p, err := store.LoadFeature(ctx.repoDir, f.Parent); err == nil && p.State != domain.StateCandidate {
			fmt.Fprintf(w, "Hint: %s is already %s: 'cerne task new \"<title>\" --parent %s' creates a task born approved, with no approve of its own (%s stays a candidate)\n", p.ID, p.State, p.ID, f.ID)
		}
	}
	printTaskAdvice(w, ctx, f.ID)
}

// printTaskAdvice is the approve check's level advice for a task, which has
// no approve (T-0007 crit. 6): a standard or advanced task declaring "não
// aplicável" hears it at birth and at start — never a refusal.
func printTaskAdvice(w io.Writer, ctx projCtx, id string) {
	if !strings.HasPrefix(id, "T-") {
		return
	}
	for _, a := range app.ApproveAdvices(ctx.repoDir, ctx.projDir, id) {
		if a.Code == app.AdviceStandardWithoutBar {
			fmt.Fprintf(w, "Advice (it never blocks the task): %s: %s\n", a.Code, a.Message)
		}
	}
}
