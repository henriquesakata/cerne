package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/spectmpl"
	"github.com/henriquesakata/cerne/internal/store"
)

func newSpecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spec",
		Short: "Author a feature's spec (LLM layer)",
	}
	cmd.AddCommand(newSpecDraftCmd())
	cmd.AddCommand(newSpecDiagramCmd())
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
	prompt := BuildSpecDraftPrompt(f, current)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nWrite the result to content/features/%s/spec.md (keeping the cerne_id frontmatter) and run 'cerne push'.\n", id)
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

// BuildSpecDraftPrompt assembles the pt-BR prompt instructing the LLM to write a
// COMPLETE feature spec from the feature's id/title/module and the current stub
// or spec content. It requests the canonical spectmpl.Sections in order and the
// authoring rules (problem-first ordering, EARS-testable criteria, the
// [ESCLARECER] ambiguity marker, code-grounded design).
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
	b.WriteString("Escreva uma spec COMPLETA e coerente para esta feature, em Markdown, com exatamente estas seções nesta ordem:\n")
	for _, s := range spectmpl.Sections {
		// F-0266: the agent-execution section is part of spectmpl.Sections now.
		b.WriteString("## " + s + "\n")
	}
	b.WriteString("\nRegras de autoria:\n")
	b.WriteString("- Abra pela Problema, ancorada em código real (arquivo.go:linha) quando possível; deixe a Verificação por último.\n")
	b.WriteString("- Critérios de aceitação numerados e testáveis; para comportamento, use EARS: \"QUANDO <condição> O SISTEMA DEVE <comportamento>\". Invariantes estruturais como asserção direta.\n")
	b.WriteString("- Marque qualquer incerteza com \"" + spectmpl.AmbiguityMarker + " pergunta específica]\" em vez de supor.\n")
	b.WriteString("- No Desenho, ancore em arquivos/pacotes reais com contratos explícitos (assinaturas, schemas); não omita o \"como\".\n")
	b.WriteString("- Inclua nas linhas de metadados \"**Nível:** <" + strings.Join(spectmpl.Levels, "|") + "> — <justificativa em 1 frase>\", avaliando a complexidade da implementação; não cite nomes de modelos ou fornecedores de LLM.\n")
	b.WriteString("- Não inclua linha de metadado de estado (Rascunho, Done, …): o ciclo de vida é dado do card (cerne status), nunca prosa que envelhece.\n")
	b.WriteString("- Inclua \"**Origem:** <o que disparou, com quem/onde e a data>\", no formato do exemplo: " + spectmpl.OrigemExample + ".\n")
	b.WriteString("- Campo de metadado cujo valor você não sabe: escreva \"" + spectmpl.UnknownMarker + "\" nele — nunca palpite; sabendo só parte, escreva a parte e \"" + spectmpl.UnknownMarker + "\" no resto. Tipo e Nível são vocabulários fechados: escolha um dos valores.\n")
	b.WriteString("- Na Execução recomendada (agente): capacidade de modelo sugerida (vocabulário genérico, coerente com o Nível) e esforço; quais tarefas manter no mesmo agente (contexto compartilhado) e por quê; quais são separáveis.\n\n")
	// F-0273: same Gauntlet contract the Backlog chat prompt uses (app.GauntletContract),
	// reused verbatim — never copied — so a spec authored via CLI carries the
	// same quality-bar protocol as one elaborated through chat.
	b.WriteString(app.GauntletContract)
	b.WriteString("\n\n")
	b.WriteString("Use o título e o módulo acima como contexto. Não inclua frontmatter (o cerne carimba o cerne_id). ")
	b.WriteString(app.DraftOutputContract + "\n")
	return b.String()
}
