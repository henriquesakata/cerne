package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/spectmpl"
)

func newFeatureCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "feature",
		Short: "Create and manage features in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newFeatureNewCmd(&dir))
	cmd.AddCommand(newFeatureListCmd(&dir))
	cmd.AddCommand(newFeatureRetagCmd(&dir))
	cmd.AddCommand(newFeatureReparentCmd(&dir))
	cmd.AddCommand(newFeatureRenameCmd(&dir))
	cmd.AddCommand(newTransitionCmds(&dir)...)
	cmd.AddCommand(newFeatureAcceptCmd(&dir))
	cmd.AddCommand(newErrataCmd(&dir))
	return cmd
}

// errFeatureContextRequired is the hard creation gate (F-0120): a candidate
// born as title-only loses its creation context the moment the session that
// minted it is gone, so the CLI refuses it and teaches the minimal path.
var errFeatureContextRequired = errors.New(
	"feature has no context: pass at least --context \"<1-3 sentences on why>\", or the full spec via --file <spec.md>/stdin")

func newFeatureNewCmd(dir *string) *cobra.Command {
	var module, parent, facet, component, file, context string
	var force bool
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a feature in candidate state",
		Long: "Creates the candidate with its context already captured: --file/stdin brings the full spec (one-shot, " +
			"nothing to fill in later) and --context seeds the draft's Problem section with why it was created. " +
			"Without one of the two, creation is refused — the title alone does not survive the session that minted it.",
		Example: "  # register the candidate with the why captured (minimum)\n" +
			"  cerne feature new \"<title>\" --context \"<1-3 sentences on why>\" [--module <m>] [--parent F-NNNN]\n" +
			"  # or with the full spec already (one-shot)\n" +
			"  cerne feature new \"<title>\" --file spec.md\n" +
			"  cerne pull\n" + lifecycleSeq,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureNew(cmd, *dir, args[0], module, parent, facet, component, file, context, force)
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "module the feature belongs to")
	cmd.Flags().StringVar(&parent, "parent", "", "parent feature ID (the epic this one decomposes)")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (CSV; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (CSV; subsystem/tool, e.g. gitback,materialize)")
	cmd.Flags().StringVar(&file, "file", "", "file with the full spec body (defaults to piped stdin)")
	cmd.Flags().StringVar(&context, "context", "", "creation context (1-3 sentences) seeded into the spec's Problem section")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard (F-0147) for a consciously distinct problem")
	return cmd
}

func runFeatureNew(cmd *cobra.Command, dir, title, module, parent, facet, component, file, context string, force bool) error {
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
	f, err := app.CreateFeatureWithSpec(ctx.repoDir, ctx.projDir, title, module, parent,
		splitInforms(facet), splitInforms(component), specBody, force)
	if err != nil {
		return err
	}
	if hasBody {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s created (candidate) with spec — already synced. Run 'cerne pull' to materialize it locally.\n", f.ID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s created (candidate) with its context seeded into the spec. Next step: complete the spec ('cerne pull' materializes the draft).\n", f.ID)
	return nil
}

func newFeatureRetagCmd(dir *string) *cobra.Command {
	var facet, component string
	cmd := &cobra.Command{
		Use:   "retag <id>",
		Short: "Redefine a feature's tags (facet/component)",
		Long: "Sets or replaces a feature's tag axes without touching the lifecycle — preserves state, " +
			"module, epic and docs. Changes only the axis passed: 'retag F-0001 --component gitback' keeps the facets; " +
			"'--facet \"\"' clears the facets. A facet outside the closed set (infra,code,data,process,auth) is rejected. " +
			"Applies to a live or implemented feature; a terminal feature (superseded/rejected) is frozen history.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureRetag(cmd, *dir, args[0], facet, component,
				cmd.Flags().Changed("facet"), cmd.Flags().Changed("component"))
		},
	}
	cmd.Flags().StringVar(&facet, "facet", "", "new facets (CSV; closed set) — empty clears")
	cmd.Flags().StringVar(&component, "component", "", "new components (CSV) — empty clears")
	return cmd
}

func runFeatureRetag(cmd *cobra.Command, dir, id, facet, component string, setFacets, setComponents bool) error {
	if !setFacets && !setComponents {
		return fmt.Errorf("pass at least --facet or --component")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	f, err := app.RetagFeature(ctx.repoDir, ctx.projDir, id,
		splitInforms(facet), splitInforms(component), setFacets, setComponents)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s retagged — facets: [%s] · components: [%s]\n",
		f.ID, strings.Join(f.Facets, ", "), strings.Join(f.Components, ", "))
	return nil
}
