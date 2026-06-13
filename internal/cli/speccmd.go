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
	"gitlab.com/rockt/cerne/internal/store"
)

func newSpecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spec",
		Short: "Author a feature's spec (LLM layer)",
	}
	cmd.AddCommand(newSpecDraftCmd())
	return cmd
}

func newSpecDraftCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "draft <id>",
		Short: "AI-draft a feature's spec",
		Long: "Rascunha a spec de uma feature usando o provider configurado. No modo delegação " +
			"(default) imprime o prompt montado para o agente hospedeiro escrever a spec; nos modos " +
			"byo-key e claude-cli gera o texto, escreve em content/features/<id>/spec.md e dá push.",
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
	f, err := store.LoadFeature(ctx.repoDir, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("feature %s não encontrada no backend", id)
		}
		return err
	}
	current := readSpecCurrent(ctx.repoDir, id)
	prompt := BuildSpecDraftPrompt(f, current)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nEscreva o resultado em content/features/%s/spec.md (mantendo o frontmatter cerne_id) e rode 'cerne push'.\n", id)
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
	if err := writeSpecDraft(ctx.repoDir, id, body); err != nil {
		return err
	}
	if err := app.CommitAndSnapshot(ctx.repoDir, ctx.projDir, "cerne: draft spec "+id); err != nil {
		return err
	}
	fmt.Fprintf(out, "spec de %s rascunhada e enviada — rode 'cerne pull' para revisar.\n", id)
	return nil
}

// readSpecCurrent returns the current content/features/<id>/spec.md, or "" when
// it is missing (a freshly created feature may have only the stub, or none).
func readSpecCurrent(repoDir, id string) string {
	data, err := os.ReadFile(featureDocPath(repoDir, id, "spec.md"))
	if err != nil {
		return ""
	}
	return string(data)
}

// writeSpecDraft writes content/features/<id>/spec.md as cerne's stamped
// cerne_id frontmatter followed by the model's trimmed body. cerne stamps the
// frontmatter itself — the model is never trusted to emit cerne_id.
func writeSpecDraft(repoDir, id, body string) error {
	path := featureDocPath(repoDir, id, "spec.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := "---\ncerne_id: " + id + "\n---\n\n" + strings.TrimSpace(body) + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

// BuildSpecDraftPrompt assembles the pt-BR prompt instructing the LLM to write a
// COMPLETE feature spec from the feature's id/title/module and the current stub
// or spec content. The four canonical sections are required.
func BuildSpecDraftPrompt(f store.Feature, currentContent string) string {
	module := f.Module
	if module == "" {
		module = "(sem módulo)"
	}
	var b strings.Builder
	b.WriteString("Você está escrevendo a especificação (spec) de uma feature de software, em português do Brasil.\n\n")
	b.WriteString("Feature:\n")
	fmt.Fprintf(&b, "- id: %s\n", f.ID)
	fmt.Fprintf(&b, "- título: %s\n", f.Title)
	fmt.Fprintf(&b, "- módulo: %s\n\n", module)
	b.WriteString("Conteúdo atual da spec (stub ou rascunho parcial; pode estar vazio):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(currentContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Escreva uma spec COMPLETA e coerente para esta feature, em Markdown, com exatamente estas seções:\n")
	b.WriteString("## Problema\n## Objetivo e critérios de sucesso\n## Decisões\n## Fora de escopo\n\n")
	b.WriteString("Use o título e o módulo acima como contexto. Não inclua frontmatter (o cerne carimba o cerne_id). ")
	b.WriteString("Responda apenas com o conteúdo da spec.\n")
	return b.String()
}
