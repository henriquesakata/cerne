package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/restmpl"
	"gitlab.com/rockt/cerne/internal/spectmpl"
	"gitlab.com/rockt/cerne/internal/store"
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
	f, err := store.LoadFeature(ctx.repoDir, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("feature %s not found in the backend", id)
		}
		return err
	}
	spec := readFeatureDoc(ctx.repoDir, id, "spec.md")
	plan := readFeatureDoc(ctx.repoDir, id, "plan.md")
	current := readFeatureDoc(ctx.repoDir, id, "result.md")
	prompt := BuildResultDraftPrompt(f, spec, plan, current)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nWrite the result to content/features/%s/result.md (keeping the cerne_id frontmatter) and run 'cerne push'.\n", id)
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
		if err := app.WriteStampedDraft(featureDocPath(ctx.repoDir, id, "result.md"), id, body); err != nil {
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

// BuildResultDraftPrompt assembles the pt-BR prompt instructing the LLM to write a
// COMPLETE result (closeout) for a feature, feeding the spec (the acceptance
// criteria to verify) and the plan (what "done" was) as context. It requests the
// canonical restmpl.Sections in order and the authoring rules: what shipped (not
// planned), binary acceptance 1:1 with the numbered spec criteria backed by
// evidence, honest debts as concrete follow-ups, the [ESCLARECER] ambiguity
// marker, and right-sizing.
func BuildResultDraftPrompt(f store.Feature, specContent, planContent, currentContent string) string {
	module := f.Module
	if module == "" {
		module = "(sem módulo)"
	}
	var b strings.Builder
	b.WriteString("Você está escrevendo o RESULTADO (registro de entrega) de uma feature de software, em português do Brasil.\n\n")
	b.WriteString("Feature:\n")
	fmt.Fprintf(&b, "- id: %s\n", f.ID)
	fmt.Fprintf(&b, "- título: %s\n", f.Title)
	fmt.Fprintf(&b, "- módulo: %s\n\n", module)
	b.WriteString("Spec (CONTEXTO — os critérios de aceitação numerados a verificar):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(specContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Plano (CONTEXTO — o que era \"pronto\"):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(planContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Conteúdo atual do resultado (stub ou rascunho parcial; pode estar vazio):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(currentContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Escreva um resultado COMPLETO e honesto, em Markdown, com exatamente estas seções nesta ordem:\n")
	for _, s := range restmpl.Sections {
		b.WriteString("## " + s + "\n")
	}
	b.WriteString("\nRegras de autoria:\n")
	b.WriteString("- Registre o que foi ENTREGUE, não o que se planejou.\n")
	b.WriteString("- Na Aceitação, verifique cada critério numerado da spec como ATENDIDO/PENDENTE com EVIDÊNCIA (nome do teste, arquivo:linha, números) — binário, 1:1, nada de \"parece pronto\".\n")
	b.WriteString("- Mostre evidência, não afirmação. Débitos e itens adiados viram follow-ups concretos (feature/caso novo), não TODO órfão.\n")
	b.WriteString("- Marque qualquer incerteza com \"" + spectmpl.AmbiguityMarker + " pergunta específica]\" em vez de supor.\n")
	b.WriteString("- Dimensione pelo trabalho: entrega pequena tem seções curtas; \"N/A\" onde não se aplica. Não infle.\n")
	b.WriteString("- Nos METADADOS, emita \"**Agente:** <id do modelo> · esforço <nível>\" com o id CONCRETO do modelo e o esforço com que VOCÊ está rodando — o nível na escala do seu harness (ex.: low/medium/high) — ou \"**Agente:** humano\"; se não souber um dos dois, escreva \"desconhecido\" só nele (ex.: \"claude-opus-5 · esforço desconhecido\"), nunca um palpite nem uma categoria genérica.\n\n")
	b.WriteString("Use a spec, o plano e o título acima como contexto. Não inclua frontmatter (o cerne carimba o cerne_id). ")
	b.WriteString("Responda apenas com o conteúdo do resultado.\n")
	return b.String()
}
