package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
)

func newDecisionCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "decision",
		Short: "Create and manage decisions (ADRs) in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(verbs.MarkChat(newDecisionNewCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newDecisionListCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newDecisionDraftCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newDecisionRetagCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newDecisionLinkCmd(&dir)))
	cmd.AddCommand(newDecisionSupersedeCmd(&dir), legacyName(newDecisionSupersedeCmd(&dir), "substituir"))
	cmd.AddCommand(verbs.MarkChat(newDecisionKeepCmd(&dir)))
	cmd.AddCommand(newDecisionRetireCmd(&dir))
	cmd.AddCommand(verbs.MarkChat(newErrataCmd(&dir)))
	return cmd
}

func newDecisionListCmd(dir *string) *cobra.Command {
	var all, asJSON bool
	var facet, component string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List decisions (ADRs) — active by default; --all includes history",
		Long: "Lists decisions read straight from the backend (store), not from the '.docs/' projection. By default shows only " +
			"the active ones; --all includes the superseded and retired (history). Each line carries id, state, title and markers " +
			"(facet/component, successor). Filter by --facet and/or --component (logical AND); --component takes a group (a,b,c), any of them, most matches first. --json for structured output.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionList(cmd, *dir, all, asJSON, facet, component)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include superseded and retired decisions (history)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable JSON output")
	cmd.Flags().StringVar(&facet, "facet", "", "filter by facet (e.g. auth)")
	cmd.Flags().StringVar(&component, "component", "", "filter by components, any of them (CSV, e.g. keycloak,gitea)")
	return cmd
}

func runDecisionList(cmd *cobra.Command, dir string, all, asJSON bool, facet, component string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	decisions, err := store.LoadAllDecisions(ctx.repoDir)
	if err != nil {
		return err
	}
	// Normalize the filters to the stored form so '--facet Auth'/'--component
	// Keycloak' match the lower/slug values persisted on the decision.
	facetQ := strings.ToLower(strings.TrimSpace(facet))
	compQ := domain.NormalizeComponents(splitInforms(component))
	w := cmd.OutOrStdout()
	shown := make([]store.Decision, 0, len(decisions))
	for _, d := range decisions {
		if !all && d.State != domain.DecisionActive {
			continue
		}
		if facetQ != "" && !slices.Contains(d.Facets, facetQ) {
			continue
		}
		shown = append(shown, d)
	}
	var matched map[string][]string
	if len(compQ) > 0 {
		shown, matched = rankByComponents(shown, func(x store.Decision) (string, []string) { return x.ID, x.Components }, compQ)
	}

	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Decisions []store.Decision    `json:"decisions"`
			Matched   map[string][]string `json:"matched,omitempty"`
		}{shown, matched})
	}

	for _, d := range shown {
		line := fmt.Sprintf("%s  [%s]  %s", d.ID, d.State, d.Title)
		var tags []string
		if len(d.Facets) > 0 {
			tags = append(tags, "facet "+strings.Join(d.Facets, ","))
		}
		if len(d.Components) > 0 {
			tags = append(tags, "comp "+strings.Join(d.Components, ","))
		}
		if d.SupersededBy != "" {
			tags = append(tags, "→ "+d.SupersededBy)
		}
		if m := matched[d.ID]; m != nil {
			tags = append(tags, "matched "+strings.Join(m, ","))
		}
		if len(tags) > 0 {
			line += "  · " + strings.Join(tags, " · ")
		}
		fmt.Fprintln(w, line)
	}
	if len(shown) == 0 {
		if all {
			fmt.Fprintln(w, "No decisions registered. Use 'cerne decision new \"<title>\"' to register an ADR.")
		} else {
			fmt.Fprintln(w, "No active decisions. Use 'cerne decision new \"<title>\"', or 'cerne decision list --all' to see history.")
		}
	}
	return nil
}

func newDecisionRetagCmd(dir *string) *cobra.Command {
	var facet, component string
	var force bool
	cmd := &cobra.Command{
		Use:   "retag <id>",
		Short: "Reset the tags (facet/component) of a decision",
		Long: "Sets or replaces the tag axes of a decision in any state, without superseding — preserves state and body. " +
			"Changes only the axis passed: 'retag D-0001 --component keycloak' keeps the facets; '--facet \"\"' clears the " +
			"facets. A facet outside the closed set (infra,code,data,process,auth) is rejected.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionRetag(cmd, *dir, args[0], facet, component,
				cmd.Flags().Changed("facet"), cmd.Flags().Changed("component"), force)
		},
	}
	cmd.Flags().StringVar(&facet, "facet", "", "new facets (CSV; closed set) — empty clears")
	cmd.Flags().StringVar(&component, "component", "", "new components (CSV) — empty clears")
	cmd.Flags().BoolVar(&force, "force", false, forceComponentHelp)
	return cmd
}

func runDecisionRetag(cmd *cobra.Command, dir, id, facet, component string, setFacets, setComponents, force bool) error {
	if !setFacets && !setComponents {
		return fmt.Errorf("provide at least --facet or --component")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	notice := componentNotice(cmd, ctx.repoDir, component)
	d, err := app.RetagDecision(ctx.repoDir, ctx.projDir, id,
		splitInforms(facet), splitInforms(component), setFacets, setComponents, force)
	if err != nil {
		return err
	}
	notice()
	fmt.Fprintf(cmd.OutOrStdout(), "%s retagged — facets: [%s] · components: [%s]\n",
		d.ID, strings.Join(d.Facets, ", "), strings.Join(d.Components, ", "))
	return nil
}

func newDecisionNewCmd(dir *string) *cobra.Command {
	var informs, file, facet, component string
	var force bool
	cmd := &cobra.Command{
		Use:   "new [title]",
		Short: "Create a decision (active); one-shot with a --file/stdin body",
		Long: "Creates an active decision. With a body (--file or piped stdin) it is one-shot: the ADR is born with " +
			"real content, already committed and pushed — no draft+push cycle. Without a body, it is born as a stub of the " +
			"template to fill in. Use --facet (closed axis: infra,code,data,process,auth) and --component " +
			"(subsystem/tool, e.g. keycloak,gitea) to organize — both multi-valued, comma-separated. " +
			"Use --informs to link the decision to the entities (F-/E-/T-/B-/D-/RB-/N-/I-) it informs: a linked decision reaches the task package (cerne context) with its body. " +
			"A live decision with a similar title refuses the creation (F-0324); --force overrides it. The title argument may be omitted when the body (--file or stdin) opens with a `# ` heading: that heading becomes the title.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionNew(cmd, *dir, args, informs, file, facet, component, force)
		},
	}
	cmd.Flags().StringVar(&informs, "informs", "", "comma-separated F-/E-/T-/B-/D-/RB-/N-/I- ids this decision informs")
	cmd.Flags().StringVar(&file, "file", "", "file with the ADR body (defaults to piped stdin); one-shot, skips draft+push")
	cmd.Flags().StringVar(&facet, "facet", "", "facets (comma-separated; closed set: infra,code,data,process,auth)")
	cmd.Flags().StringVar(&component, "component", "", "components (comma-separated subsystem/tool tags, e.g. keycloak,gitea)")
	cmd.Flags().BoolVar(&force, "force", false, "bypass the duplicate-registration guard (F-0324) for a consciously distinct decision, and accept a component near an existing one (E-0005; check 'cerne tags' first)")
	return cmd
}

func runDecisionNew(cmd *cobra.Command, dir string, args []string, informs, file, facet, component string, force bool) error {
	body, err := readOptionalBody(cmd, file)
	if err != nil {
		return err
	}
	title, err := newTitle(args, body)
	if err != nil {
		return err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("the decision title cannot be empty")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	notice := componentNotice(cmd, ctx.repoDir, component)
	d, err := app.CreateDecision(ctx.repoDir, ctx.projDir, title, body, splitInforms(informs), splitInforms(facet), splitInforms(component), force)
	if err != nil {
		return err
	}
	notice()
	// A decision created with a body is already real content, committed and pushed
	// by CreateDecision (CommitAndSnapshot) — nothing to draft or push. A bodyless
	// decision is a stub: keep the draft + push next step.
	if strings.TrimSpace(body) != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s created (active) with a body — already synced. Run 'cerne pull' to materialize locally.\n", d.ID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s created (active). Next step: fill in the ADR ('cerne decision draft %s' scaffolds it) and run 'cerne push'.\n", d.ID, d.ID)
	return nil
}

// newDecisionLinkCmd is note link for a decision (E-0005 criterion 22): a
// decision linked to a task reaches its package in ring (a), with its body.
func newDecisionLinkCmd(dir *string) *cobra.Command {
	var informs, unlink string
	cmd := &cobra.Command{
		Use:   "link <id>",
		Short: "Add or remove the entities an active decision informs",
		Long: "Adds (--informs) or removes (--unlink) the F-/E-/T-/B-/D-/RB-/N-/I- targets of an active decision, without " +
			"duplicating the ones already there. A linked decision reaches the agent of the task it informs: the task " +
			"package (cerne context) prints it with its body. Prints the final list.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(informs) == "" && strings.TrimSpace(unlink) == "" {
				return fmt.Errorf("provide at least --informs or --unlink")
			}
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			d, absent, err := app.LinkDecision(ctx.repoDir, ctx.projDir, args[0], splitInforms(informs), splitInforms(unlink))
			if err != nil {
				return err
			}
			if len(absent) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "not present: %s\n", strings.Join(absent, ", "))
			}
			list := "(none)"
			if len(d.Informs) > 0 {
				list = strings.Join(d.Informs, ", ")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s informs: %s\n", d.ID, list)
			return nil
		},
	}
	cmd.Flags().StringVar(&informs, "informs", "", "comma-separated F-/E-/T-/B-/D-/RB-/N-/I- ids to add")
	cmd.Flags().StringVar(&unlink, "unlink", "", "comma-separated ids to remove")
	return cmd
}

func newDecisionSupersedeCmd(dir *string) *cobra.Command {
	var by, reason string
	cmd := &cobra.Command{
		Use:   "supersede <id>",
		Short: "Supersede one decision with another (requires --by and --reason)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionSupersede(cmd, *dir, args[0], by, reason)
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "id of the decision that supersedes")
	cmd.Flags().StringVar(&reason, "reason", "", "reason for the supersession")
	return verbs.MarkLifecycle(cmd)
}

func runDecisionSupersede(cmd *cobra.Command, dir, id, by, reason string) error {
	// Validate flags before resolveProject so bad invocations fail fast
	// without touching origin.
	if err := app.ValidateTransitionFlags(domain.ActionSupersede, by, reason); err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	out, err := app.TransitionDecision(ctx.repoDir, ctx.projDir, id, by, reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", out.ID, out.From, out.To)
	return nil
}

func newDecisionKeepCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "keep <id>",
		Short: "Record that an active decision was reviewed and still holds",
		Long: "Stamps the review date (and the actor, on the decision.kept event) of an active decision: the human " +
			"verdict that it still holds. 'cerne lint' (stale-decision) counts from the review, so the decision is " +
			"flagged again only on a new signal — N more days with no live citation, or a newer active decision " +
			"carrying all its components. To take it out of the current state, use 'cerne decision retire' or 'supersede'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			d, err := app.KeepDecision(ctx.repoDir, ctx.projDir, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s reviewed — 'cerne lint' (stale-decision) counts from now until a new signal.\n", d.ID)
			return nil
		},
	}
}

func newDecisionRetireCmd(dir *string) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "retire <id>",
		Short: "Retire a decision that no longer applies (no successor; --reason required)",
		Long: "Moves an active decision to retired WITHOUT a successor — for a decision that stopped applying and " +
			"has nothing to point --by at. It leaves the current state (.docs/) on the next 'cerne pull' and stays " +
			"searchable history; the reason goes on the decision.retired event. To replace it with a newer decision, " +
			"use 'cerne decision supersede --by'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Refuse before resolveProject: a bad invocation never touches origin.
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required: %w", domain.ErrRetireReasonRequired)
			}
			ctx, err := resolveProject(*dir)
			if err != nil {
				return err
			}
			out, err := app.RetireDecision(ctx.repoDir, ctx.projDir, args[0], reason)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s retired: %s → %s\n", out.ID, out.From, out.To)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the decision no longer applies (required)")
	return verbs.MarkLifecycle(cmd)
}

func newDecisionDraftCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "draft <id>",
		Short: "AI-draft a decision's ADR",
		Long: "Drafts a decision's ADR using the configured provider. In delegation mode (default) it " +
			"prints the assembled prompt for the host agent to write the ADR; in byo-key and claude-cli modes " +
			"it generates the text, writes it to content/decisions/<id>.md and pushes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionDraft(cmd, *dir, args[0])
		},
	}
}

func runDecisionDraft(cmd *cobra.Command, dir, id string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	d, err := store.LoadDecision(ctx.repoDir, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("decision %s not found in the backend", id)
		}
		return err
	}
	current := readDecisionCurrent(ctx.repoDir, id)
	prompt := BuildDecisionDraftPrompt(store.DocsLanguage(ctx.repoDir), d, current)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nWrite the result to content/decisions/%s.md (keeping the cerne_id frontmatter) and run 'cerne push'.\n", id)
		return nil
	}

	// byo-key / claude-cli: synthesize, write the ADR, commit and push.
	prov, err := llm.NewProvider(ctx.cfg.LLM)
	if err != nil {
		return err
	}
	body, err := prov.Complete(cmd.Context(), prompt)
	if err != nil {
		return err
	}
	// Write and commit inside the backend lock (F-0122); the provider call
	// above stays outside it.
	err = app.MutateAndSnapshot(ctx.repoDir, ctx.projDir, func() (string, error) {
		if err := app.WriteStampedDraft(app.DecisionDocPath(ctx.repoDir, id), id, "decision", body); err != nil {
			return "", err
		}
		return "cerne: draft decision " + id, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s's ADR drafted and pushed — run 'cerne pull' to review.\n", id)
	return nil
}

// readDecisionCurrent returns the current content/decisions/<id>.md, or "" when
// it is missing.
func readDecisionCurrent(repoDir, id string) string {
	data, err := os.ReadFile(app.DecisionDocPath(repoDir, id))
	if err != nil {
		return ""
	}
	return string(data)
}

// BuildDecisionDraftPrompt assembles the English prompt instructing the LLM to write
// a COMPLETE ADR from the decision's id/title and the current stub or content. It
// requests the canonical dectmpl.Sections in order and the authoring rules: named
// forces in the context, the decision stated imperatively, negative consequences
// always listed, alternatives with why-rejected, the ambiguity
// marker, and right-sizing (status is cerne data, never a prose section).
func BuildDecisionDraftPrompt(l doclang.Lang, d store.Decision, currentContent string) string {
	var b strings.Builder
	b.WriteString("You are writing a software architecture decision record (ADR).\n\n")
	b.WriteString("Decision:\n")
	fmt.Fprintf(&b, "- id: %s\n", d.ID)
	fmt.Fprintf(&b, "- title: %s\n\n", d.Title)
	b.WriteString("Current ADR content (stub or partial draft; may be empty):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(currentContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Write a COMPLETE, coherent ADR, in Markdown, with exactly these sections in this order:\n")
	writeSections(&b, doclang.Decision, l)
	b.WriteString("\nAuthoring rules:\n")
	b.WriteString("- In " + heading(doclang.Decision, l, "Context") + ", name the forces and constraints in tension and the trigger (why now); anchor in the real (code, case).\n")
	b.WriteString("- State the " + heading(doclang.Decision, l, "Decision") + " in the imperative (" + doclang.Examples(l).DecisionVoice + ") with the 2-3 reasons that made it win.\n")
	b.WriteString("- In " + heading(doclang.Decision, l, "Consequences") + ", ALWAYS list the negatives/trade-offs and the constraints/invariants, not only the benefits.\n")
	b.WriteString("- In " + heading(doclang.Decision, l, "Alternatives considered") + ", give 2+ options with pros/cons and why they were rejected.\n")
	b.WriteString("- Mark any uncertainty with the ambiguity marker instead of guessing.\n")
	b.WriteString("- Size by the decision: if trivial, " + heading(doclang.Decision, l, "Alternatives considered") + "/" + heading(doclang.Decision, l, "Implementation and relations") + " may be \"N/A\". Do not write a status section (cerne manages active/superseded).\n")
	b.WriteString("- The title records the decision taken, not the question (" + doclang.Examples(l).DecisionTitle + "). An accepted ADR is a historical record — to change course, supersede it; do not rewrite it.\n\n")
	b.WriteString("Do not include frontmatter (cerne stamps the cerne_id). " + app.DraftOutputContract + "\n")
	b.WriteString(doclang.Directive(l))
	return b.String()
}
