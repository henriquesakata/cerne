package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
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
	cmd.AddCommand(verbs.MarkChat(newFeatureListCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newFeatureRetagCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newFeatureSetCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newFeatureReparentCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newFeatureCapabilityCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newFeatureRenameCmd(&dir)))
	cmd.AddCommand(newFeatureSpecCmd(&dir))
	cmd.AddCommand(verbs.MarkLocal(newFeatureAmendCmd(&dir)))
	cmd.AddCommand(newTransitionCmds(&dir)...)
	cmd.AddCommand(newFeatureDeliverCmd(&dir))
	cmd.AddCommand(newFeatureAcceptCmd(&dir))
	cmd.AddCommand(newFeatureLearningsCmd(&dir))
	cmd.AddCommand(verbs.MarkChat(newErrataCmd(&dir)))
	return cmd
}

// errFeatureContextRequired is the hard creation gate (F-0120): a candidate
// born as title-only loses its creation context the moment the session that
// minted it is gone, so the CLI refuses it and teaches the minimal path.
var errFeatureContextRequired = errors.New(
	"feature has no context: pass at least --context \"<1-3 sentences on why>\", or the full spec via --file <spec.md>/stdin")

func newFeatureNewCmd(dir *string) *cobra.Command {
	var module, parent, fixes, facet, component, file, context, owner, priority string
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
			return runFeatureNew(cmd, *dir, args[0], module, parent, fixes, facet, component, file, context, force, owner, priority)
		},
	}
	cmd.Flags().StringVar(&module, "module", "", "module the feature belongs to")
	cmd.Flags().StringVar(&parent, "parent", "", "parent feature ID (the epic this one decomposes)")
	cmd.Flags().StringVar(&fixes, "fixes", "", "feature ID whose delivered defect this one fixes (F-0321)")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (CSV; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (CSV; subsystem/tool, e.g. gitback,materialize)")
	cmd.Flags().StringVar(&file, "file", "", "file with the full spec body (defaults to piped stdin)")
	cmd.Flags().StringVar(&context, "context", "", "creation context (1-3 sentences) seeded into the spec's Problem section")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard (F-0147) for a consciously distinct problem")
	cmd.Flags().StringVar(&owner, "owner", "", "responsible handle (free text, F-0204)")
	cmd.Flags().StringVar(&priority, "priority", "", "priority (closed set: p0,p1,p2,p3, F-0204)")
	return verbs.MarkChatTool(cmd)
}

func runFeatureNew(cmd *cobra.Command, dir, title, module, parent, fixes, facet, component, file, context string, force bool, owner, priority string) error {
	return runFeatureLikeNew(cmd, dir, title, module, parent, facet, component, file, context, force, owner, priority,
		func(repoDir, projDir, title, module, parent string, facets, components []string, specBody string, force bool, owner, priority string) (store.Feature, error) {
			return app.CreateFeatureFixes(repoDir, projDir, title, module, parent, fixes, facets, components, specBody, force, owner, priority)
		})
}

func newFeatureSpecCmd(dir *string) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "spec <id>",
		Short: "Rewrite a candidate's or an approved feature's spec from a file or stdin",
		Long: "Replaces the whole spec.md of a candidate or an approved feature — the same gated write the Backlog " +
			"chat performs on confirmation (F-0256), now reachable from the CLI so an agent can update a feature it did " +
			"not just create. Touches the document only: no state transition, no retitle. On an approved feature a spec " +
			"that differs from the approved one stales the approval: 'cerne feature start' is refused until a human " +
			"re-approves (F-0318). Past approval it is refused.",
		Example: "  cerne feature spec F-0001 --file spec.md\n" +
			"  cat spec.md | cerne feature spec F-0001",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureSpec(cmd, *dir, args[0], file)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "file with the full spec body (defaults to piped stdin)")
	return verbs.MarkChatTool(cmd)
}

func runFeatureSpec(cmd *cobra.Command, dir, id, file string) error {
	body, err := readOptionalBody(cmd, file)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("empty spec body: pass the full spec via --file <spec.md> or on stdin")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	f, changed, err := app.UpdateCandidateSpec(ctx.repoDir, ctx.projDir, id, body)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintf(cmd.OutOrStdout(), "%s spec unchanged — nothing to update.\n", f.ID)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s spec updated (%s) — already synced. Run 'cerne pull' to materialize it locally.\n", f.ID, f.State)
	}
	if stale, _, _ := lint.SpecChangedAfterApproval(ctx.repoDir, f); stale {
		fmt.Fprintln(cmd.OutOrStdout(), approvalStaleLine(f.ID))
	}
	printApproveReadiness(cmd.OutOrStdout(), ctx.repoDir, ctx.projDir, f.ID)
	return nil
}

func newFeatureAmendCmd(dir *string) *cobra.Command {
	var file, reason, doc string
	cmd := &cobra.Command{
		Use:   "amend <id>",
		Short: "Replace a feature document with a reason: the result before the accept, or the spec/plan while approved or in development",
		Long: "Replaces a whole feature document, with the reason on the feature.doc_edited event. Three forms:\n" +
			"  --doc result (default): the result.md of an IMPLEMENTED feature — the correction window between " +
			"'complete' and the human 'accept' (F-0305); the new result must pass the result checks (evidence per " +
			"criterion, Gauntlet record).\n" +
			"  --doc spec | --doc plan: the spec or plan of an APPROVED or IN-DEVELOPMENT feature (F-0338). Write the " +
			"amendment into the criterion or task it changes — a body with an appended amendment section is refused. " +
			"The replaced text stays readable as a revision ('cerne show <id> --doc <doc> --rev N'); amending an " +
			"approved spec stales the approval until a human re-approves.\n" +
			"Start from the stored text: 'cerne show <id> --doc <doc> > <doc>.md'. Before the approve a spec is a " +
			"draft (pull/push); anything else is corrected with 'cerne feature errata'.",
		Example: "  cerne feature amend F-0001 --file result.md --reason \"criterion 2 had no evidence\"\n" +
			"  cerne feature amend F-0001 --doc spec --file spec.md --reason \"<why>\"\n" +
			"  cat plan.md | cerne feature amend F-0001 --doc plan --reason \"<why>\"",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureAmend(cmd, *dir, args[0], doc, file, reason)
		},
	}
	cmd.Flags().StringVar(&doc, "doc", "result", "document to amend: result|spec|plan")
	cmd.Flags().StringVar(&file, "file", "", "file with the full document body (defaults to piped stdin)")
	cmd.Flags().StringVar(&reason, "reason", "", "why the document is amended (required; recorded on the event)")
	return cmd
}

func runFeatureAmend(cmd *cobra.Command, dir, id, doc, file, reason string) error {
	body, err := readOptionalBody(cmd, file)
	if err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	if doc != "result" {
		f, rev, changed, err := app.AmendFeatureDoc(ctx.repoDir, ctx.projDir, id, doc, body, reason)
		if err != nil {
			return err
		}
		if !changed {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s unchanged — nothing to amend.\n", f.ID, doc)
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s %s amended (%s) — revision %d keeps the previous text: 'cerne show %s --doc %s --rev %d'. Run 'cerne pull' to refresh .docs/.\n",
			f.ID, doc, f.State, rev, f.ID, doc, rev)
		return nil
	}
	f, changed, err := app.AmendFeatureResult(ctx.repoDir, ctx.projDir, id, body, reason)
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintf(cmd.OutOrStdout(), "%s result unchanged — nothing to amend.\n", f.ID)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s result amended (%s) — already synced. Read it with 'cerne show %s --doc result'.\n", f.ID, f.State, f.ID)
	}
	// An amend read from the .docs/ copy leaves that copy behind: push refuses it
	// until it is removed, so say so (F-0307).
	if abs, err := filepath.Abs(file); file != "" && err == nil {
		if rel, err := filepath.Rel(filepath.Join(ctx.projDir, ".docs"), abs); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			fmt.Fprintf(cmd.OutOrStdout(), "Next: remove .docs/%s now — the local copy keeps blocking 'cerne push' until it is gone.\n", filepath.ToSlash(rel))
		}
	}
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

func newFeatureSetCmd(dir *string) *cobra.Command {
	var owner, priority, fixes string
	cmd := &cobra.Command{
		Use:   "set <id>",
		Short: "Set a feature's owner, priority and/or fixes link in place (F-0204, F-0321)",
		Long: "Changes owner and/or priority without touching the lifecycle — preserves state, module, tags and docs. " +
			"Changes only the flag passed: 'set F-0001 --priority p1' keeps the owner; '--owner \"\"' clears the owner. " +
			"A priority outside the closed set (p0,p1,p2,p3) is rejected, nothing written. Applies to a live or " +
			"implemented feature; a terminal feature (done/rejected/superseded) is frozen history.",
		Example: "  cerne feature set F-0001 --owner ana --priority p1\n" +
			"  cerne feature set F-0001 --priority p0   # owner untouched\n" +
			"  cerne feature set F-0002 --fixes F-0001  # F-0002 corrects a defect F-0001 shipped",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFeatureSet(cmd, *dir, args[0], owner, priority, fixes,
				cmd.Flags().Changed("owner"), cmd.Flags().Changed("priority"), cmd.Flags().Changed("fixes"))
		},
	}
	cmd.Flags().StringVar(&owner, "owner", "", "new owner (free text) — empty clears")
	cmd.Flags().StringVar(&priority, "priority", "", "new priority (closed set: p0,p1,p2,p3) — empty clears")
	cmd.Flags().StringVar(&fixes, "fixes", "", "feature ID this one fixes — empty clears")
	return cmd
}

func runFeatureSet(cmd *cobra.Command, dir, id, owner, priority, fixes string, setOwner, setPriority, setFixes bool) error {
	if !setOwner && !setPriority && !setFixes {
		return fmt.Errorf("pass at least --owner, --priority or --fixes")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	f, err := app.SetFeature(ctx.repoDir, ctx.projDir, id, owner, priority, fixes, setOwner, setPriority, setFixes)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s set — owner: %q · priority: %q · fixes: %q\n", f.ID, f.Owner, f.Priority, f.Fixes)
	return nil
}
