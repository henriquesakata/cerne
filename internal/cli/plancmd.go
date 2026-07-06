package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/plantmpl"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
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
		Long: "Rascunha o plano de execução de uma feature (com spec já aprovada) usando o provider " +
			"configurado. No modo delegação (default) imprime o prompt montado para o agente hospedeiro " +
			"escrever o plano; nos modos byo-key e claude-cli gera o texto, escreve em " +
			"content/features/<id>/plan.md e dá push.",
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
	f, err := store.LoadFeature(ctx.repoDir, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("feature %s não encontrada no backend", id)
		}
		return err
	}
	spec := readFeatureDoc(ctx.repoDir, id, "spec.md")
	current := readFeatureDoc(ctx.repoDir, id, "plan.md")
	prompt := BuildPlanDraftPrompt(f, spec, current)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nEscreva o resultado em content/features/%s/plan.md (mantendo o frontmatter cerne_id) e rode 'cerne push'.\n", id)
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
		if err := writePlanDraft(ctx.repoDir, id, body); err != nil {
			return "", err
		}
		return "cerne: draft plan " + id, nil
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "plano de %s rascunhado e enviado — rode 'cerne pull' para revisar.\n", id)
	return nil
}

// readFeatureDoc returns content/features/<id>/<file>, or "" when it is missing.
func readFeatureDoc(repoDir, id, file string) string {
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "features", id, file))
	if err != nil {
		return ""
	}
	return string(data)
}

// writePlanDraft writes content/features/<id>/plan.md as cerne's stamped cerne_id
// frontmatter followed by the model's trimmed body. cerne stamps the frontmatter
// itself — the model is never trusted to emit cerne_id.
func writePlanDraft(repoDir, id, body string) error {
	path := filepath.Join(repoDir, "content", "features", id, "plan.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + strings.TrimSpace(body) + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// BuildPlanDraftPrompt assembles the pt-BR prompt instructing the LLM to write a
// COMPLETE execution plan for a feature whose spec is already approved. It feeds
// the feature id/title/module, the approved spec (as context to reference, not
// repeat) and the current plan stub or draft, then requests the canonical
// plantmpl.Sections in order plus the authoring rules: small ordered test-first
// tasks, verification tied to a numbered spec criterion, exact commit messages,
// the [ESCLARECER] ambiguity marker, right-sizing, and a binary definition of done.
func BuildPlanDraftPrompt(f store.Feature, specContent, currentContent string) string {
	module := f.Module
	if module == "" {
		module = "(sem módulo)"
	}
	var b strings.Builder
	b.WriteString("Você está escrevendo o PLANO DE EXECUÇÃO de uma feature de software (a spec já foi aprovada), em português do Brasil.\n\n")
	b.WriteString("Feature:\n")
	fmt.Fprintf(&b, "- id: %s\n", f.ID)
	fmt.Fprintf(&b, "- título: %s\n", f.Title)
	fmt.Fprintf(&b, "- módulo: %s\n\n", module)
	b.WriteString("Spec aprovada (CONTEXTO — referencie e satisfaça seus critérios numerados; NÃO a repita):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(specContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Conteúdo atual do plano (stub ou rascunho parcial; pode estar vazio):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(currentContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Escreva um plano COMPLETO e executável, em Markdown, com exatamente estas seções nesta ordem:\n")
	for _, s := range plantmpl.Sections {
		b.WriteString("## " + s + "\n")
	}
	b.WriteString("\nRegras de autoria:\n")
	b.WriteString("- Não repita a spec; referencie-a. O plano descreve COMO construir.\n")
	b.WriteString("- Quebre em tarefas pequenas e ORDENADAS; cada tarefa deixa o repositório verde. Por tarefa: arquivos reais (criar/editar/remover), teste primeiro (TDD), verificação (comando → saída esperada) citando o número do critério da spec, e a mensagem de commit Conventional exata. Marque [paralela] quando a tarefa for independente.\n")
	b.WriteString("- Dimensione pelo trabalho: mudança trivial (≤3 arquivos) cabe em uma única tarefa; seções que não se aplicam viram uma linha (\"N/A\"). Não infle o plano.\n")
	b.WriteString("- Marque qualquer incerteza com \"" + spectmpl.AmbiguityMarker + " pergunta específica]\" em vez de supor.\n")
	b.WriteString("- A Definição de pronto é um checklist binário (ATENDIDO/PENDENTE) com evidência arquivo:linha: cada critério da spec com implementação e um teste que o prova.\n\n")
	b.WriteString("Use o título, o módulo e a spec acima como contexto. Não inclua frontmatter (o cerne carimba o cerne_id). ")
	b.WriteString("Responda apenas com o conteúdo do plano.\n")
	return b.String()
}
