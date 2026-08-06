package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
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
	cmd.AddCommand(newEpicNewCmd(&dir))
	return cmd
}

func newEpicNewCmd(dir *string) *cobra.Command {
	var module, parent, facet, component, file, context string
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
			return runFeatureLikeNew(cmd, *dir, args[0], module, parent, facet, component, file, context, force,
				app.CreateEpicWithSpec)
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "module the epic belongs to")
	cmd.Flags().StringVar(&parent, "parent", "", "parent feature ID (an epic can nest under another)")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (CSV; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (CSV; subsystem/tool, e.g. gitback,materialize)")
	cmd.Flags().StringVar(&file, "file", "", "file with the full spec body (defaults to piped stdin)")
	cmd.Flags().StringVar(&context, "context", "", "creation context (1-3 sentences) seeded into the spec's Problem section")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard (F-0147) for a consciously distinct problem")
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
	cmd.AddCommand(newTaskNewCmd(&dir))
	return cmd
}

func newTaskNewCmd(dir *string) *cobra.Command {
	var module, parent, facet, component, file, context string
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
			return runFeatureLikeNew(cmd, *dir, args[0], module, parent, facet, component, file, context, force,
				app.CreateTaskWithSpec)
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "module the task belongs to")
	cmd.Flags().StringVar(&parent, "parent", "", "the committed F-/E- this task decomposes (required)")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (CSV; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (CSV; subsystem/tool, e.g. gitback,materialize)")
	cmd.Flags().StringVar(&file, "file", "", "file with the full spec body (defaults to piped stdin)")
	cmd.Flags().StringVar(&context, "context", "", "creation context (1-3 sentences) seeded into the spec's Problem section")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard (F-0147) for a consciously distinct problem")
	return cmd
}

// runFeatureLikeNew is runFeatureNew generalized over the creation core
// (F-0203): the same context gate, spec seeding and messages for F-/E-/T-.
func runFeatureLikeNew(cmd *cobra.Command, dir, title, module, parent, facet, component, file, context string, force bool,
	create func(repoDir, projDir, title, module, parent string, facets, components []string, specBody string, force bool) (store.Feature, error),
) error {
	body, err := readOptionalBody(cmd, file)
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
		specBody = spectmpl.StubWithContext(title, context)
	}
	f, err := create(ctx.repoDir, ctx.projDir, title, module, parent,
		splitInforms(facet), splitInforms(component), specBody, force)
	if err != nil {
		return err
	}
	if hasBody {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s created (%s) with spec — already synced. Run 'cerne pull' to materialize it locally.\n", f.ID, f.State)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s created (%s) with its context seeded into the spec. Next step: complete the spec ('cerne pull' materializes the draft).\n", f.ID, f.State)
	return nil
}
