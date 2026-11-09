package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/runtmpl"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
	"gitlab.com/rockt/cerne/internal/verbs"
)

func newRunbookCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "runbook",
		Short: "Create and manage runbooks (operational procedures) in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(verbs.MarkChat(newRunbookNewCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newRunbookDraftCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newRunbookRetagCmd(&dir)))
	cmd.AddCommand(newRunbookSubstituirCmd(&dir))
	cmd.AddCommand(newRunbookRetireCmd(&dir))
	cmd.AddCommand(verbs.MarkLocal(newRunbookMigrateCmd(&dir)))
	cmd.AddCommand(verbs.MarkChat(newErrataCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newRunbookDecideCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newRunbookMergeCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newRunbookRunCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newRunbookStepCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newRunbookReportCmd(&dir)))
	cmd.AddCommand(verbs.MarkLocal(newRunbookPreflightCmd(&dir)))
	return cmd
}

func newRunbookRetagCmd(dir *string) *cobra.Command {
	var facet, component string
	cmd := &cobra.Command{
		Use:   "retag <id>",
		Short: "Reset the tags (facet/component) of an active runbook",
		Long: "Sets or replaces the tag axes of an active runbook, without superseding it — preserves body and " +
			"state. Changes only the axis passed: 'retag RB-0001 --component gitback' keeps the facets; '--facet \"\"' " +
			"clears the facets. A facet outside the closed set (infra,code,data,process,auth) is rejected.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunbookRetag(cmd, *dir, args[0], facet, component,
				cmd.Flags().Changed("facet"), cmd.Flags().Changed("component"))
		},
	}
	cmd.Flags().StringVar(&facet, "facet", "", "new facets (CSV; closed set) — empty clears")
	cmd.Flags().StringVar(&component, "component", "", "new components (CSV) — empty clears")
	return cmd
}

func runRunbookRetag(cmd *cobra.Command, dir, id, facet, component string, setFacets, setComponents bool) error {
	if !setFacets && !setComponents {
		return fmt.Errorf("provide at least --facet or --component")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	rb, err := app.RetagRunbook(ctx.repoDir, ctx.projDir, id,
		splitInforms(facet), splitInforms(component), setFacets, setComponents)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s retagged — facets: [%s] · components: [%s]\n",
		rb.ID, strings.Join(rb.Facets, ", "), strings.Join(rb.Components, ", "))
	return nil
}

func newRunbookMigrateCmd(dir *string) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Reclassify references imported from docs/runbooks into runbooks",
		Long: "Finds references whose doc was imported from docs/runbooks/ (before the runbook existed) and " +
			"reclassifies them into runbooks: mints an RB- id, moves the content, rewrites the cross-links (R-→RB-, " +
			"references/→runbooks/) and removes the old reference. Without --yes, it only lists what it would do.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunbookMigrate(cmd, *dir, yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "apply the migration (without this, dry-run)")
	return cmd
}

func runRunbookMigrate(cmd *cobra.Command, dir string, yes bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if !yes {
		plan, err := app.PlanRunbookMigration(ctx.repoDir)
		if err != nil {
			return err
		}
		if len(plan) == 0 {
			fmt.Fprintln(out, "No docs/runbooks references to migrate.")
			return nil
		}
		fmt.Fprintf(out, "%d reference(s) would become runbooks (use --yes to apply):\n", len(plan))
		for _, m := range plan {
			fmt.Fprintf(out, "  %s  %s  (%s)\n", m.FromRef, m.Title, m.Source)
		}
		return nil
	}
	// Scan, convert and commit inside the backend lock (F-0122).
	var migrated []app.RunbookMigration
	mErr := app.MutateAndSnapshot(ctx.repoDir, ctx.projDir, func() (string, error) {
		var err error
		migrated, err = app.MigrateRunbooksFromReferences(ctx.repoDir)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: migrate %d runbook(s) from references", len(migrated)), nil
	})
	if errors.Is(mErr, gitback.ErrNothingToCommit) || len(migrated) == 0 {
		fmt.Fprintln(out, "No docs/runbooks references to migrate.")
		return nil
	}
	if mErr != nil {
		return mErr
	}
	fmt.Fprintf(out, "Migrated %d reference(s) to runbooks:\n", len(migrated))
	for _, m := range migrated {
		fmt.Fprintf(out, "  %s → %s  %s\n", m.FromRef, m.ToRunbook, m.Title)
	}
	fmt.Fprintln(out, "Run 'cerne pull' to materialize.")
	return nil
}

func newRunbookNewCmd(dir *string) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a runbook (active); one-shot with a --file/stdin body",
		Long: "Creates an active runbook. With a body (--file or piped stdin) it is one-shot: born with real " +
			"content, already committed and pushed — no draft+push cycle. Without a body, it is born as a stub of the " +
			"template to fill in.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunbookNew(cmd, *dir, args[0], file)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "file with the runbook body (defaults to piped stdin); one-shot, skips draft+push")
	return cmd
}

func runRunbookNew(cmd *cobra.Command, dir, title, file string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("the runbook title cannot be empty")
	}
	body, err := readOptionalBody(cmd, file)
	if err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	rb, err := app.CreateRunbook(ctx.repoDir, ctx.projDir, title, body)
	if err != nil {
		return err
	}
	// A runbook created with a body is already real content, committed and pushed
	// by CreateRunbook (CommitAndSnapshot) — nothing to draft or push. A bodyless
	// runbook is a stub: keep the draft + push next step.
	if strings.TrimSpace(body) != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s created (active) with a body — already synced. Run 'cerne pull' to materialize locally.\n", rb.ID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s created (active). Next step: fill in the runbook ('cerne runbook draft %s' scaffolds it) and run 'cerne push'.\n", rb.ID, rb.ID)
	return nil
}

func newRunbookSubstituirCmd(dir *string) *cobra.Command {
	var by, reason string
	cmd := &cobra.Command{
		Use:   "substituir <id>",
		Short: "Supersede one runbook with another (requires --by and --reason)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunbookSubstituir(cmd, *dir, args[0], by, reason)
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "id of the runbook that supersedes")
	cmd.Flags().StringVar(&reason, "reason", "", "reason for the supersession")
	return verbs.MarkLifecycle(cmd)
}

func runRunbookSubstituir(cmd *cobra.Command, dir, id, by, reason string) error {
	// Validate flags before resolveProject so bad invocations fail fast
	// without touching origin.
	if err := app.ValidateTransitionFlags(domain.ActionSupersede, by, reason); err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	out, err := app.TransitionRunbook(ctx.repoDir, ctx.projDir, id, by, reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", out.ID, out.From, out.To)
	return nil
}

func newRunbookRetireCmd(dir *string) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "retire <id>",
		Short: "Retire an obsolete runbook (goes to superseded, no successor; --reason required)",
		Long: "Moves a runbook from active to superseded WITHOUT a successor — for a procedure that no longer holds " +
			"(e.g. it became an explanation/note, or the flow was discontinued). The runbook leaves the current state " +
			"(.docs/CONTEXT.md) and becomes searchable history ('cerne search'/'ask'). To swap it for another " +
			"runbook, use 'cerne runbook substituir --by'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunbookRetire(cmd, *dir, args[0], reason)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "reason for retiring (required)")
	return verbs.MarkLifecycle(cmd)
}

func runRunbookRetire(cmd *cobra.Command, dir, id, reason string) error {
	// Validate the required reason before resolveProject so bad invocations
	// fail fast without touching origin. retire is substituir with no successor.
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("--reason is required")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	out, err := app.TransitionRunbook(ctx.repoDir, ctx.projDir, id, "", reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s retired: %s → %s\n", out.ID, out.From, out.To)
	return nil
}

func newRunbookDraftCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "draft <id>",
		Short: "AI-draft a runbook",
		Long: "Drafts a runbook using the configured provider. In delegation mode (default) it prints the " +
			"assembled prompt for the host agent to write the runbook; in byo-key and claude-cli modes it generates " +
			"the text, writes it to content/runbooks/<id>.md and pushes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunbookDraft(cmd, *dir, args[0])
		},
	}
}

func runRunbookDraft(cmd *cobra.Command, dir, id string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	rb, err := store.LoadRunbook(ctx.repoDir, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("runbook %s not found in the backend", id)
		}
		return err
	}
	current := readRunbookCurrent(ctx.repoDir, id)
	prompt := BuildRunbookDraftPrompt(rb, current)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nWrite the result to content/runbooks/%s.md (keeping the cerne_id frontmatter) and run 'cerne push'.\n", id)
		return nil
	}

	// byo-key / claude-cli: synthesize, write the runbook, commit and push.
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
		if err := app.WriteStampedDraft(app.RunbookDocPath(ctx.repoDir, id), id, "runbook", body); err != nil {
			return "", err
		}
		return "cerne: draft runbook " + id, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "runbook %s drafted and pushed — run 'cerne pull' to review.\n", id)
	return nil
}

// readRunbookCurrent returns the current content/runbooks/<id>.md, or "" when it
// is missing.
func readRunbookCurrent(repoDir, id string) string {
	data, err := os.ReadFile(app.RunbookDocPath(repoDir, id))
	if err != nil {
		return ""
	}
	return string(data)
}

// BuildRunbookDraftPrompt assembles the pt-BR prompt instructing the LLM to write
// a COMPLETE runbook from the runbook's id/title and the current stub or content.
// It requests the canonical runtmpl.Sections in order and the authoring rules:
// a clear trigger, numbered actionable steps with expected output, prerequisites,
// verification, rollback and escalation, the [ESCLARECER] ambiguity marker, and
// right-sizing.
func BuildRunbookDraftPrompt(rb store.Runbook, currentContent string) string {
	var b strings.Builder
	b.WriteString("Você está escrevendo um runbook (procedimento operacional acionável) de software, em português do Brasil.\n\n")
	b.WriteString("Runbook:\n")
	fmt.Fprintf(&b, "- id: %s\n", rb.ID)
	fmt.Fprintf(&b, "- título: %s\n\n", rb.Title)
	b.WriteString("Conteúdo atual do runbook (stub ou rascunho parcial; pode estar vazio):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(currentContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Escreva um runbook COMPLETO e acionável, em Markdown, com exatamente estas seções nesta ordem:\n")
	for _, s := range runtmpl.Sections {
		b.WriteString("## " + s + "\n")
	}
	b.WriteString("\nRegras de autoria:\n")
	b.WriteString("- Em Quando usar, deixe claro o gatilho/sintomas para o leitor confirmar em segundos que é o runbook certo.\n")
	b.WriteString("- No Procedimento, passos numerados, ação primeiro, comandos copiáveis, cada um com a SAÍDA ESPERADA (\"você deve ver: X\").\n")
	b.WriteString("- Inclua Pré-requisitos (acessos/ferramentas/estado), Verificação (como confirmar sucesso) e Rollback e troubleshooting (como desfazer + a quem escalar).\n")
	b.WriteString("- Marque qualquer incerteza com \"" + spectmpl.AmbiguityMarker + " pergunta específica]\" em vez de supor.\n")
	b.WriteString("- Dimensione pela tarefa: runbook simples tem seções curtas; \"N/A\" onde não se aplica. Não infle.\n\n")
	b.WriteString("Não inclua frontmatter (o cerne carimba o cerne_id). " + app.DraftOutputContract + "\n")
	return b.String()
}
