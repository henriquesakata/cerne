package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/store"
)

func newSpecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spec",
		Short: "Author a feature's spec (LLM layer)",
	}
	cmd.AddCommand(newSpecDraftCmd())
	cmd.AddCommand(newSpecDiagramCmd())
	cmd.AddCommand(newSpecCheckCmd())
	return cmd
}

func newSpecDraftCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "draft <id>",
		Short: "AI-draft a feature's spec",
		Long: "Drafts a feature's spec using the configured provider. In delegation mode " +
			"(default) it prints the assembled prompt for the host agent to write the spec; in " +
			"byo-key and claude-cli modes it generates the text, writes it to content/features/<id>/spec.md and pushes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSpecDraft(cmd, dir, args[0])
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

func runSpecDraft(cmd *cobra.Command, dir, id string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	f, err := app.CheckDocWritable(ctx.repoDir, id, "spec") // a closed feature's documents are history (F-0305)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("feature %s not found in the backend", id)
		}
		return err
	}
	current := readSpecCurrent(ctx.repoDir, id)
	l := store.DocsLanguage(ctx.repoDir)
	prompt := BuildSpecDraftPrompt(l, f, current)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nWrite the result into the spec, keeping the cerne_id frontmatter: %s.\n", app.EditDoc(f, "spec.md"))
		return nil
	}

	// byo-key / claude-cli: synthesize, write the spec, commit and push.
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
		if _, err := app.CheckDocWritable(ctx.repoDir, id, "spec"); err != nil {
			return "", err // closed while the provider was drafting
		}
		if err := app.WriteStampedDraft(specPath(ctx.repoDir, id), id, "spec", body); err != nil {
			return "", err
		}
		if err := app.RecordDocWrite(ctx.repoDir, id, "spec", app.LLMAuthorAnswered(ctx.cfg.LLM, llm.AnsweredModel(prov))); err != nil {
			return "", err
		}
		return "cerne: draft spec " + id, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s's spec drafted and pushed — run 'cerne pull' to review.\n", id)
	return nil
}

// specPath is the backend path of a feature's spec document.
func specPath(repoDir, id string) string {
	return filepath.Join(repoDir, "content", "features", id, "spec.md")
}

// readSpecCurrent returns the current content/features/<id>/spec.md, or "" when
// it is missing (a freshly created feature may have only the stub, or none).
func readSpecCurrent(repoDir, id string) string {
	data, err := os.ReadFile(specPath(repoDir, id))
	if err != nil {
		return ""
	}
	return string(data)
}

// BuildSpecDraftPrompt assembles the English prompt instructing the LLM to write a
// COMPLETE feature spec from the feature's id/title/module and the current stub
// or spec content. It requests the canonical spectmpl.Sections in order and the
// authoring rules (problem-first ordering, EARS-testable criteria, the
// ambiguity marker, code-grounded design).
func BuildSpecDraftPrompt(l doclang.Lang, f store.Feature, currentContent string) string {
	module := f.Module
	if module == "" {
		module = "(no module)"
	}
	var b strings.Builder
	b.WriteString("You are writing the specification (spec) of a software feature.\n\n")
	b.WriteString("Feature:\n")
	fmt.Fprintf(&b, "- id: %s\n", f.ID)
	fmt.Fprintf(&b, "- title: %s\n", f.Title)
	fmt.Fprintf(&b, "- module: %s\n\n", module)
	b.WriteString("Current spec content (stub or partial draft; may be empty):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(currentContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Write a COMPLETE, coherent spec for this feature, in Markdown, with exactly these sections in this order:\n")
	writeSections(&b, doclang.Spec, l)
	b.WriteString("\nAuthoring rules:\n")
	b.WriteString("- Open with " + heading(doclang.Spec, l, "Problem") + ", anchored in real code (file.go:line) when possible; leave " + heading(doclang.Spec, l, "Verification") + " last.\n")
	b.WriteString("- Numbered, testable acceptance criteria; for behaviour use EARS, in the form given in the language note at the end. Structural invariants as a direct assertion.\n")
	b.WriteString("- Mark any uncertainty with the ambiguity marker instead of guessing.\n")
	b.WriteString("- In " + heading(doclang.Spec, l, "Design") + ", anchor in real files/packages with explicit contracts (signatures, schemas); do not omit the \"how\".\n")
	b.WriteString("- Include in the metadata lines \"" + doclang.Examples(l).LevelLine + "\", judging the complexity of the implementation; do not name LLM models or vendors.\n")
	b.WriteString("- Do not include a status metadata line (Draft, Done, ...): the lifecycle is card data (cerne status), never prose that ages.\n")
	b.WriteString("- Include the Origin line: what triggered it, with whom/where and the date, in the shape of this example line: " + doclang.Examples(l).OriginExample + ".\n")
	b.WriteString("- A metadata field whose value you do not know: write the unknown value in it — never guess; knowing only part, write the part and the unknown value for the rest. Type and Level are closed vocabularies: choose one of the values.\n")
	b.WriteString("- In " + heading(doclang.Spec, l, "Recommended execution (agent)") + ": the suggested model capability (generic vocabulary, consistent with the Level) and effort; which tasks to keep in the same agent (shared context) and why; which are separable.\n\n")
	// F-0273: same Gauntlet contract the Backlog chat prompt uses (app.GauntletContract),
	// reused — never copied — so a spec authored via CLI carries the
	// same quality-bar protocol as one elaborated through chat.
	b.WriteString(app.GauntletContract(l))
	b.WriteString("\n\n")
	b.WriteString("Use the title and module above as context. Do not include frontmatter (cerne stamps the cerne_id). ")
	b.WriteString(app.DraftOutputContract + "\n")
	b.WriteString(doclang.Directive(l))
	return b.String()
}

var errSpecCheckFailed = errors.New("the spec has errors")

func newSpecCheckCmd() *cobra.Command {
	var dir, file string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check a draft spec body without writing anything",
		Long: "Reads a spec body (--file or stdin) and lists what the approve would flag: " +
			"missing template sections, a missing or malformed Gauntlet bar line (skipped in light mode) " +
			"and open clarifications (warnings). Exits 1 on an error, 0 otherwise.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := runSpecCheck(cmd, dir, file, asJSON)
			if asJSON && err != nil && !errors.Is(err, errSpecCheckFailed) {
				data, _ := json.Marshal(map[string]string{"error": err.Error()})
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
			}
			return err
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	cmd.Flags().StringVar(&file, "file", "", "file with the spec body (defaults to stdin)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the issues as JSON")
	return cmd
}

func runSpecCheck(cmd *cobra.Command, dir, file string, asJSON bool) error {
	body, err := readOptionalBody(cmd, file)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return errors.New("no spec body: pass --file or pipe it on stdin")
	}
	ctx, err := resolvePaths(dir) // local only: nothing is fetched or written
	if err != nil {
		return err
	}
	issues := doccheck.CheckSpecBody([]byte(doccheck.ProposalSpecBody(body)), store.DocsLanguage(ctx.repoDir), !ctx.cfg.Gates.GauntletEnabled())
	out := cmd.OutOrStdout()
	failed := false
	for _, is := range issues {
		failed = failed || is.Severity == doccheck.SeverityError
	}
	if asJSON {
		if issues == nil {
			issues = []doccheck.Issue{}
		}
		data, err := json.Marshal(issues)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(data))
	} else {
		for _, is := range issues {
			fmt.Fprintf(out, "%s: %s: %s\n", is.Severity, is.Code, is.Detail)
		}
		if len(issues) == 0 {
			fmt.Fprintln(out, "No format issues found.")
		}
		// Advice never changes the exit code (F-0517); --json keeps its issues array.
		for _, a := range app.ClarifyAdvices([]byte(doccheck.ProposalSpecBody(body)), store.DocsLanguage(ctx.repoDir)) {
			fmt.Fprintf(out, "advice: %s: %s\n", a.Code, a.Message)
		}
	}
	if failed {
		return errSpecCheckFailed
	}
	return nil
}
