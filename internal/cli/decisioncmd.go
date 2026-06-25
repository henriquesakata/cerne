package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/dectmpl"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
)

func newDecisionCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "decision",
		Short: "Create and manage decisions (ADRs) in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newDecisionNewCmd(&dir))
	cmd.AddCommand(newDecisionDraftCmd(&dir))
	cmd.AddCommand(newDecisionSubstituirCmd(&dir))
	return cmd
}

func newDecisionNewCmd(dir *string) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a decision (active); one-shot with a --file/stdin body",
		Long: "Cria uma decisão ativa. Com corpo (--file ou stdin com pipe) é one-shot: o ADR nasce com " +
			"conteúdo real, já commitado e enviado — sem o ciclo draft+push. Sem corpo, nasce como stub do " +
			"template para preencher.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionNew(cmd, *dir, args[0], file)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "file with the ADR body (defaults to piped stdin); one-shot, skips draft+push")
	return cmd
}

func runDecisionNew(cmd *cobra.Command, dir, title, file string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("o título da decisão não pode ser vazio")
	}
	body, err := readOptionalBody(cmd, file)
	if err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	d, err := app.CreateDecision(ctx.repoDir, ctx.projDir, title, body)
	if err != nil {
		return err
	}
	// A decision created with a body is already real content, committed and pushed
	// by CreateDecision (CommitAndSnapshot) — nothing to draft or push. A bodyless
	// decision is a stub: keep the draft + push next step.
	if strings.TrimSpace(body) != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s criada (active) com corpo — já sincronizada. Rode 'cerne pull' para materializar localmente.\n", d.ID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s criada (active). Próximo passo: preencha o ADR ('cerne decision draft %s' ajuda) e rode 'cerne push'.\n", d.ID, d.ID)
	return nil
}

func newDecisionSubstituirCmd(dir *string) *cobra.Command {
	var by, reason string
	cmd := &cobra.Command{
		Use:   "substituir <id>",
		Short: "Substituir uma decisão por outra (requer --by e --reason)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDecisionSubstituir(cmd, *dir, args[0], by, reason)
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "id da decisão que substitui")
	cmd.Flags().StringVar(&reason, "reason", "", "motivo da substituição")
	return cmd
}

func runDecisionSubstituir(cmd *cobra.Command, dir, id, by, reason string) error {
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

func newDecisionDraftCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "draft <id>",
		Short: "AI-draft a decision's ADR",
		Long: "Rascunha o ADR de uma decisão usando o provider configurado. No modo delegação (default) " +
			"imprime o prompt montado para o agente hospedeiro escrever o ADR; nos modos byo-key e claude-cli " +
			"gera o texto, escreve em content/decisions/<id>.md e dá push.",
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
			return fmt.Errorf("decisão %s não encontrada no backend", id)
		}
		return err
	}
	current := readDecisionCurrent(ctx.repoDir, id)
	prompt := BuildDecisionDraftPrompt(d, current)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nEscreva o resultado em content/decisions/%s.md (mantendo o frontmatter cerne_id) e rode 'cerne push'.\n", id)
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
	if err := writeDecisionDraft(ctx.repoDir, id, body); err != nil {
		return err
	}
	if err := app.CommitAndSnapshot(ctx.repoDir, ctx.projDir, "cerne: draft decision "+id); err != nil {
		return err
	}
	fmt.Fprintf(out, "ADR de %s rascunhado e enviado — rode 'cerne pull' para revisar.\n", id)
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

// writeDecisionDraft writes content/decisions/<id>.md as cerne's stamped cerne_id
// frontmatter followed by the model's trimmed body. cerne stamps the frontmatter
// itself — the model is never trusted to emit cerne_id.
func writeDecisionDraft(repoDir, id, body string) error {
	path := app.DecisionDocPath(repoDir, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + strings.TrimSpace(body) + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// BuildDecisionDraftPrompt assembles the pt-BR prompt instructing the LLM to write
// a COMPLETE ADR from the decision's id/title and the current stub or content. It
// requests the canonical dectmpl.Sections in order and the authoring rules: named
// forces in the context, the decision stated imperatively, negative consequences
// always listed, alternatives with why-rejected, the [ESCLARECER] ambiguity
// marker, and right-sizing (status is cerne data, never a prose section).
func BuildDecisionDraftPrompt(d store.Decision, currentContent string) string {
	var b strings.Builder
	b.WriteString("Você está escrevendo um registro de decisão de arquitetura (ADR) de software, em português do Brasil.\n\n")
	b.WriteString("Decisão:\n")
	fmt.Fprintf(&b, "- id: %s\n", d.ID)
	fmt.Fprintf(&b, "- título: %s\n\n", d.Title)
	b.WriteString("Conteúdo atual do ADR (stub ou rascunho parcial; pode estar vazio):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(currentContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Escreva um ADR COMPLETO e coerente, em Markdown, com exatamente estas seções nesta ordem:\n")
	for _, s := range dectmpl.Sections {
		b.WriteString("## " + s + "\n")
	}
	b.WriteString("\nRegras de autoria:\n")
	b.WriteString("- No Contexto, nomeie as forças e restrições em tensão e o gatilho (por que agora); ancore no real (código, caso).\n")
	b.WriteString("- Enuncie a Decisão no imperativo (\"Vamos X\" / \"Usaremos X\") com as 2-3 razões que a fizeram vencer.\n")
	b.WriteString("- Em Consequências, liste SEMPRE as negativas/trade-offs e as restrições/invariantes, não só os benefícios.\n")
	b.WriteString("- Em Alternativas consideradas, dê 2+ opções com prós/contras e por que foram rejeitadas.\n")
	b.WriteString("- Marque qualquer incerteza com \"" + spectmpl.AmbiguityMarker + " pergunta específica]\" em vez de supor.\n")
	b.WriteString("- Dimensione pela decisão: se trivial, Alternativas/Implementação podem ser \"N/A\". Não escreva seção de status (o cerne gerencia active/superseded).\n")
	b.WriteString("- O título registra a decisão tomada, não a pergunta (\"Usar X\", não \"Devemos usar X?\"). Um ADR aceito é registro histórico — para mudar de rumo, supersede; não reescreva.\n\n")
	b.WriteString("Não inclua frontmatter (o cerne carimba o cerne_id). Responda apenas com o conteúdo do ADR.\n")
	return b.String()
}
