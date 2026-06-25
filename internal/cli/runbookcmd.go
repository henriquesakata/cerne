package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/runtmpl"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

func newRunbookCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "runbook",
		Short: "Create and manage runbooks (operational procedures) in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newRunbookNewCmd(&dir))
	cmd.AddCommand(newRunbookDraftCmd(&dir))
	cmd.AddCommand(newRunbookSubstituirCmd(&dir))
	cmd.AddCommand(newRunbookMigrateCmd(&dir))
	return cmd
}

func newRunbookMigrateCmd(dir *string) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Reclassifica referências importadas de docs/runbooks em runbooks",
		Long: "Encontra referências cujo doc foi importado de docs/runbooks/ (antes de o runbook existir) e as " +
			"reclassifica em runbooks: cunha um id RB-, move o conteúdo, reescreve os cross-links (R-→RB-, " +
			"references/→runbooks/) e remove a referência antiga. Sem --yes, apenas lista o que faria.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunbookMigrate(cmd, *dir, yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "aplica a migração (sem isto, dry-run)")
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
			fmt.Fprintln(out, "Nenhuma referência de docs/runbooks para migrar.")
			return nil
		}
		fmt.Fprintf(out, "%d referência(s) virariam runbooks (use --yes para aplicar):\n", len(plan))
		for _, m := range plan {
			fmt.Fprintf(out, "  %s  %s  (%s)\n", m.FromRef, m.Title, m.Source)
		}
		return nil
	}
	migrated, err := app.MigrateRunbooksFromReferences(ctx.repoDir)
	if err != nil {
		return err
	}
	if len(migrated) == 0 {
		fmt.Fprintln(out, "Nenhuma referência de docs/runbooks para migrar.")
		return nil
	}
	if err := app.CommitAndSnapshot(ctx.repoDir, ctx.projDir, fmt.Sprintf("cerne: migrate %d runbook(s) from references", len(migrated))); err != nil {
		return err
	}
	fmt.Fprintf(out, "Migradas %d referência(s) para runbooks:\n", len(migrated))
	for _, m := range migrated {
		fmt.Fprintf(out, "  %s → %s  %s\n", m.FromRef, m.ToRunbook, m.Title)
	}
	fmt.Fprintln(out, "Rode 'cerne pull' para materializar.")
	return nil
}

func newRunbookNewCmd(dir *string) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a runbook (active); one-shot with a --file/stdin body",
		Long: "Cria um runbook ativo. Com corpo (--file ou stdin com pipe) é one-shot: nasce com conteúdo " +
			"real, já commitado e enviado — sem o ciclo draft+push. Sem corpo, nasce como stub do template " +
			"para preencher.",
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
		return fmt.Errorf("o título do runbook não pode ser vazio")
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
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s criado (active) com corpo — já sincronizado. Rode 'cerne pull' para materializar localmente.\n", rb.ID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s criado (active). Próximo passo: preencha o runbook ('cerne runbook draft %s' ajuda) e rode 'cerne push'.\n", rb.ID, rb.ID)
	return nil
}

func newRunbookSubstituirCmd(dir *string) *cobra.Command {
	var by, reason string
	cmd := &cobra.Command{
		Use:   "substituir <id>",
		Short: "Substituir um runbook por outro (requer --by e --reason)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunbookSubstituir(cmd, *dir, args[0], by, reason)
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "id do runbook que substitui")
	cmd.Flags().StringVar(&reason, "reason", "", "motivo da substituição")
	return cmd
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

func newRunbookDraftCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "draft <id>",
		Short: "AI-draft a runbook",
		Long: "Rascunha um runbook usando o provider configurado. No modo delegação (default) imprime o " +
			"prompt montado para o agente hospedeiro escrever o runbook; nos modos byo-key e claude-cli gera " +
			"o texto, escreve em content/runbooks/<id>.md e dá push.",
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
			return fmt.Errorf("runbook %s não encontrado no backend", id)
		}
		return err
	}
	current := readRunbookCurrent(ctx.repoDir, id)
	prompt := BuildRunbookDraftPrompt(rb, current)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nEscreva o resultado em content/runbooks/%s.md (mantendo o frontmatter cerne_id) e rode 'cerne push'.\n", id)
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
	if err := writeRunbookDraft(ctx.repoDir, id, body); err != nil {
		return err
	}
	if err := app.CommitAndSnapshot(ctx.repoDir, ctx.projDir, "cerne: draft runbook "+id); err != nil {
		return err
	}
	fmt.Fprintf(out, "runbook %s rascunhado e enviado — rode 'cerne pull' para revisar.\n", id)
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

// writeRunbookDraft writes content/runbooks/<id>.md as cerne's stamped cerne_id
// frontmatter followed by the model's trimmed body. cerne stamps the frontmatter
// itself — the model is never trusted to emit cerne_id.
func writeRunbookDraft(repoDir, id, body string) error {
	path := app.RunbookDocPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + strings.TrimSpace(body) + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
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
	b.WriteString("Não inclua frontmatter (o cerne carimba o cerne_id). Responda apenas com o conteúdo do runbook.\n")
	return b.String()
}
