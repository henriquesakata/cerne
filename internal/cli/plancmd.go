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
	prompt := BuildPlanDraftPrompt(f, spec, current, children)
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

// BuildPlanDraftPrompt assembles the pt-BR prompt instructing the LLM to write a
// COMPLETE execution plan for a feature whose spec is already approved. It feeds
// the feature id/title/module, the approved spec (as context to reference, not
// repeat) and the current plan stub or draft, then requests the canonical
// plantmpl.Sections in order plus the authoring rules: small ordered test-first
// tasks, verification tied to a numbered spec criterion, exact commit messages,
// the [ESCLARECER] ambiguity marker, right-sizing, and a binary definition of done.
// children, when non-empty, makes the prompt list the roster (id, title,
// state) and ask for the canonical "Etapa N: ID (título)" execution map
// (F-0213 criterion 3) — mesma etapa = paralelas, ordem/paralelismo justificados.
func BuildPlanDraftPrompt(f store.Feature, specContent, currentContent string, children []store.Feature) string {
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
	if len(children) > 0 {
		b.WriteString("Esta feature é um ÉPICO — já tem estas tasks/filhas mintadas:\n")
		for _, c := range children {
			fmt.Fprintf(&b, "- %s (%s) — estado: %s\n", c.ID, c.Title, c.State)
		}
		b.WriteString("Na seção \"Dependências e ordem\", inclua um Mapa de execução no formato canônico — uma linha " +
			"por etapa, filhos da mesma etapa são paralelos:\n")
		b.WriteString("  Etapa 1: T-0001 (título)\n  Etapa 2 (paralelas): T-0002 (título) · T-0003 (título)\n")
		b.WriteString("Justifique a ordem/paralelismo escolhido.\n\n")
	}
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
	b.WriteString(app.DraftOutputContract + "\n")
	return b.String()
}
