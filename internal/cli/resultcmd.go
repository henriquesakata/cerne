package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

func newResultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "result",
		Short: "Author a feature's result/closeout (LLM layer)",
	}
	cmd.AddCommand(newResultDraftCmd())
	return cmd
}

func newResultDraftCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "draft <id>",
		Short: "AI-draft a feature's result",
		Long: "Drafts a feature's result (delivery record) using the configured provider, " +
			"with the spec and plan as context. In delegation mode (default) it prints the assembled prompt for the " +
			"host agent to write the result; in byo-key and claude-cli modes it generates the text, writes it to " +
			"content/features/<id>/result.md and pushes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResultDraft(cmd, dir, args[0])
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

func runResultDraft(cmd *cobra.Command, dir, id string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	f, err := app.CheckDocWritable(ctx.repoDir, id, "result") // a closed feature's documents are history (F-0305)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("feature %s not found in the backend", id)
		}
		return err
	}
	spec := readFeatureDoc(ctx.repoDir, id, "spec.md")
	plan := readFeatureDoc(ctx.repoDir, id, "plan.md")
	current := readFeatureDoc(ctx.repoDir, id, "result.md")
	prompt := BuildResultDraftPrompt(store.DocsLanguage(ctx.repoDir), f, spec, plan, current)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nWrite the result into the result, keeping the cerne_id frontmatter: %s.\n", app.EditDoc(f, "result.md"))
		return nil
	}

	// byo-key / claude-cli: synthesize, write the result, commit and push.
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
		if _, err := app.CheckDocWritable(ctx.repoDir, id, "result"); err != nil {
			return "", err // closed while the provider was drafting
		}
		if err := app.WriteStampedDraft(featureDocPath(ctx.repoDir, id, "result.md"), id, "result", body); err != nil {
			return "", err
		}
		if err := app.RecordDocWrite(ctx.repoDir, id, "result", app.LLMAuthorAnswered(ctx.cfg.LLM, llm.AnsweredModel(prov))); err != nil {
			return "", err
		}
		return "cerne: draft result " + id, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s's result drafted and pushed — run 'cerne pull' to review.\n", id)
	return nil
}

// BuildResultDraftPrompt assembles the English prompt instructing the LLM to write a
// COMPLETE result (closeout) for a feature, feeding the spec (the acceptance
// criteria to verify) and the plan (what "done" was) as context. It requests the
// canonical restmpl.Sections in order and the authoring rules: what shipped (not
// planned), binary acceptance 1:1 with the numbered spec criteria backed by
// evidence, honest debts as concrete follow-ups, the ambiguity
// marker, and right-sizing.
func BuildResultDraftPrompt(l doclang.Lang, f store.Feature, specContent, planContent, currentContent string) string {
	module := f.Module
	if module == "" {
		module = "(no module)"
	}
	d := func(t doclang.Term) string { return doclang.Display(t, l) }
	var b strings.Builder
	b.WriteString("You are writing the RESULT (delivery record) of a software feature.\n\n")
	b.WriteString("Feature:\n")
	fmt.Fprintf(&b, "- id: %s\n", f.ID)
	fmt.Fprintf(&b, "- title: %s\n", f.Title)
	fmt.Fprintf(&b, "- module: %s\n\n", module)
	b.WriteString("Spec (CONTEXT — the numbered acceptance criteria to verify):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(specContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Plan (CONTEXT — what \"done\" was):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(planContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Current result content (stub or partial draft; may be empty):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(currentContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Write a COMPLETE, honest result, in Markdown, with exactly these sections in this order:\n")
	writeSections(&b, doclang.Result, l)
	b.WriteString("\nAuthoring rules:\n")
	b.WriteString("- Record what was DELIVERED, not what was planned.\n")
	b.WriteString("- In " + heading(doclang.Result, l, "Acceptance") + ", verify each numbered spec criterion as met/unmet (the marks of the language note) with EVIDENCE (test name, file:line, numbers) — binary, 1:1, no \"looks done\".\n")
	b.WriteString("- Show evidence, not assertion. Debts and deferred items become concrete follow-ups (a new feature/case), not an orphan TODO.\n")
	b.WriteString("- Mark any uncertainty with the ambiguity marker instead of guessing.\n")
	b.WriteString("- Size by the work: a small delivery has short sections; \"N/A\" where it does not apply (never in the " + heading(doclang.Result, l, "Gauntlet") + " section). Do not inflate.\n")
	b.WriteString("- In the " + heading(doclang.Result, l, "Gauntlet") + " section, transcribe only the real record of the loop found in the current content or in the plan: the `" + d(doclang.BarLabel) + ":` line, one line `" + d(doclang.Round) + " N — <verdict>: \"<quote of the judge>\"` per round and the `" + d(doclang.CriticFindings) + ":` line. " +
		"cerne feature complete checks this record: never invent a round, a verdict, a judge quote or a waiver. Without the record, write in the section only the ambiguity marker followed by a question naming what is missing (the bar, the rounds with the judge's quoted verdict, the disposition of the findings) and a closing bracket.\n")
	b.WriteString("- In the METADATA, emit the Agent line with the CONCRETE id of the model and the effort YOU are running with — the level on your harness's scale (e.g. low/medium/high) — or the human form; if you do not know one of the two, write the unknown value only in it (e.g. the id \"" + project.LLMConfig{}.WithDefaults().Model + "\" with the effort unknown), never a guess nor a generic category.\n\n")
	b.WriteString("Use the spec, the plan and the title above as context. Do not include frontmatter (cerne stamps the cerne_id). ")
	b.WriteString(app.DraftOutputContract + "\n")
	b.WriteString(doclang.Directive(l))
	return b.String()
}
