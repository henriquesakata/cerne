package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/store"
)

func newPlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Author a feature's execution plan (LLM layer)",
	}
	cmd.AddCommand(newPlanDraftCmd())
	return cmd
}

func newPlanDraftCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "draft <id>",
		Short: "AI-draft a feature's execution plan",
		Long: "Drafts a feature's execution plan (with the spec already approved) using the configured " +
			"provider. In delegation mode (default) it prints the assembled prompt for the host agent to " +
			"write the plan; in byo-key and claude-cli modes it generates the text, writes it to " +
			"content/features/<id>/plan.md and pushes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlanDraft(cmd, dir, args[0])
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

func runPlanDraft(cmd *cobra.Command, dir, id string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	f, err := app.CheckDocWritable(ctx.repoDir, id, "plan") // a closed feature's documents are history (F-0305)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("feature %s not found in the backend", id)
		}
		return err
	}
	spec := readFeatureDoc(ctx.repoDir, id, "spec.md")
	current := readFeatureDoc(ctx.repoDir, id, "plan.md")
	feats, err := store.LoadAllFeatures(ctx.repoDir)
	if err != nil {
		return err
	}
	children := store.ChildrenByParent(feats)[id]
	prompt := BuildPlanDraftPrompt(store.DocsLanguage(ctx.repoDir), f, spec, current, children)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nWrite the result to content/features/%s/plan.md (keeping the cerne_id frontmatter) and run 'cerne push'.\n", id)
		return nil
	}

	// byo-key / claude-cli: synthesize, write the plan, commit and push.
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
		if _, err := app.CheckDocWritable(ctx.repoDir, id, "plan"); err != nil {
			return "", err // closed while the provider was drafting
		}
		if err := app.WriteStampedDraft(featureDocPath(ctx.repoDir, id, "plan.md"), id, "plan", body); err != nil {
			return "", err
		}
		if err := app.RecordDocWrite(ctx.repoDir, id, "plan", app.LLMAuthorAnswered(ctx.cfg.LLM, llm.AnsweredModel(prov))); err != nil {
			return "", err
		}
		return "cerne: draft plan " + id, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s's plan drafted and pushed — run 'cerne pull' to review.\n", id)
	return nil
}

// featureDocPath is the backend path of one of a feature's lifecycle documents.
func featureDocPath(repoDir, id, file string) string {
	return filepath.Join(repoDir, "content", "features", id, file)
}

// readFeatureDoc returns content/features/<id>/<file>, or "" when it is missing.
func readFeatureDoc(repoDir, id, file string) string {
	data, err := os.ReadFile(featureDocPath(repoDir, id, file))
	if err != nil {
		return ""
	}
	return string(data)
}

// BuildPlanDraftPrompt assembles the English prompt instructing the LLM to write a
// COMPLETE execution plan for a feature whose spec is already approved. It feeds
// the feature id/title/module, the approved spec (as context to reference, not
// repeat) and the current plan stub or draft, then requests the canonical
// plantmpl.Sections in order plus the authoring rules: small ordered test-first
// tasks, verification tied to a numbered spec criterion, exact commit messages,
// the ambiguity marker, right-sizing, and a binary definition of done.
// children, when non-empty, makes the prompt list the roster (id, title,
// state) and ask for the canonical "Etapa N: ID (title)" execution map
// (F-0213 criterion 3) — same stage = parallel, order/parallelism justified.
func BuildPlanDraftPrompt(l doclang.Lang, f store.Feature, specContent, currentContent string, children []store.Feature) string {
	module := f.Module
	if module == "" {
		module = "(no module)"
	}
	const (
		secDeps = 2
		secDone = 5
	)
	var b strings.Builder
	b.WriteString("You are writing the EXECUTION PLAN of a software feature (the spec is already approved).\n\n")
	b.WriteString("Feature:\n")
	fmt.Fprintf(&b, "- id: %s\n", f.ID)
	fmt.Fprintf(&b, "- title: %s\n", f.Title)
	fmt.Fprintf(&b, "- module: %s\n\n", module)
	b.WriteString("Approved spec (CONTEXT — reference it and satisfy its numbered criteria; do NOT repeat it):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(specContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Current plan content (stub or partial draft; may be empty):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(currentContent))
	b.WriteString("\n---\n\n")
	if len(children) > 0 {
		b.WriteString("This feature is an EPIC — it already has these tasks/children minted:\n")
		for _, c := range children {
			fmt.Fprintf(&b, "- %s (%s) — state: %s\n", c.ID, c.Title, c.State)
		}
		b.WriteString("In the " + heading(doclang.Plan, l, secDeps) + " section, include an execution map in the canonical format — one line " +
			"per stage, children of the same stage are parallel (the words Etapa and paralelas are parsed, keep them exactly):\n")
		b.WriteString("  Etapa 1: T-0001 (title)\n  Etapa 2 (paralelas): T-0002 (title) · T-0003 (title)\n")
		b.WriteString("Justify the chosen order and parallelism.\n\n")
	}
	b.WriteString("Write a COMPLETE, executable plan, in Markdown, with exactly these sections in this order:\n")
	writeSections(&b, doclang.Plan, l)
	b.WriteString("\nAuthoring rules:\n")
	b.WriteString("- Do not repeat the spec; reference it. The plan describes HOW to build.\n")
	b.WriteString("- Break the work into small, ORDERED tasks; each task leaves the repository green. Per task: real files (create/edit/remove), test first (TDD), verification (command → expected output) citing the number of the spec criterion, and the exact Conventional Commit message. Mark [parallel] when a task is independent.\n")
	b.WriteString("- Size by the work: a trivial change (<=3 files) fits in a single task; sections that do not apply become one line (\"N/A\"). Do not inflate the plan.\n")
	b.WriteString("- Mark any uncertainty with the ambiguity marker instead of guessing.\n")
	b.WriteString("- " + heading(doclang.Plan, l, secDone) + " is a binary checklist (met/unmet marks of the language note) with file:line evidence: each spec criterion with an implementation and a test that proves it.\n\n")
	b.WriteString("Use the title, module and spec above as context. Do not include frontmatter (cerne stamps the cerne_id). ")
	b.WriteString(app.DraftOutputContract + "\n")
	b.WriteString(doclang.Directive(l))
	return b.String()
}
