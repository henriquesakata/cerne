package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/store"
)

func newSpecDiagramCmd() *cobra.Command {
	var dir, name string
	cmd := &cobra.Command{
		Use:   "diagram <id>",
		Short: "AI-draft a Mermaid sequence diagram for a feature",
		Long: "Gera um diagrama de sequência em Mermaid para uma feature usando o provider " +
			"configurado. No modo delegação (default) imprime o prompt montado para o agente " +
			"hospedeiro escrever o diagrama; nos modos byo-key e claude-cli gera o Mermaid, " +
			"salva em content/artifacts/<id>/<name>.mmd e dá push.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSpecDiagram(cmd, dir, args[0], name)
		},
	}
	cmd.Flags().StringVar(&name, "name", "sequence", "artifact name (normalized to <name>.mmd)")
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

func runSpecDiagram(cmd *cobra.Command, dir, id, rawName string) error {
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
	name := app.MermaidArtifactName(rawName)
	spec := readSpecCurrent(ctx.repoDir, id)
	prompt := app.BuildSequenceDiagramPrompt(f, spec)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nEscreva o resultado em content/artifacts/%s/%s e rode 'cerne push'.\n", id, name)
		return nil
	}

	// byo-key / claude-cli: synthesize, sanitize, save the artifact, commit and push.
	prov, err := llm.NewProvider(ctx.cfg.LLM)
	if err != nil {
		return err
	}
	body, err := prov.Complete(cmd.Context(), prompt)
	if err != nil {
		return err
	}
	mermaid, err := app.SanitizeMermaid(body)
	if err != nil {
		if errors.Is(err, app.ErrNotMermaidSequence) {
			return fmt.Errorf("o modelo não retornou um diagrama de sequência Mermaid para %s — tente novamente", id)
		}
		return err
	}
	if err := app.SaveArtifact(ctx.repoDir, ctx.projDir, id, name, []byte(mermaid+"\n")); err != nil {
		return err
	}
	fmt.Fprintf(out, "diagrama de sequência de %s rascunhado e enviado (artefato %s) — rode 'cerne pull' para revisar.\n", id, name)
	return nil
}
