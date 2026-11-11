package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/store"
)

func newSpecDiagramCmd() *cobra.Command {
	var dir, name string
	cmd := &cobra.Command{
		Use:   "diagram <id>",
		Short: "AI-draft a Mermaid sequence diagram for a feature",
		Long: "Generates a Mermaid sequence diagram for a feature using the configured " +
			"provider. In delegation mode (default) it prints the assembled prompt for the host " +
			"agent to write the diagram; in byo-key and claude-cli modes it generates the Mermaid, " +
			"saves it to content/artifacts/<id>/<name>.mmd and pushes it.",
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
			return fmt.Errorf("feature %s not found in the backend", id)
		}
		return err
	}
	name := app.MermaidArtifactName(rawName)
	spec := readSpecCurrent(ctx.repoDir, id)
	// Project structure grounds the diagram in real components (best-effort:
	// a read failure just omits the structure, never aborts generation).
	structure, _ := app.ProjectStructure(ctx.projDir)
	prompt := app.BuildSequenceDiagramPrompt(f, spec, structure)
	out := cmd.OutOrStdout()

	// delegation (default): print the prompt for the host agent to draft inline.
	if llm.ResolveMode(ctx.cfg.LLM.Mode) == llm.ModeDelegation {
		fmt.Fprint(out, prompt)
		fmt.Fprintf(out, "\n\nWrite the result to content/artifacts/%s/%s and run 'cerne push'.\n", id, name)
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
			return fmt.Errorf("the model did not return a Mermaid sequence diagram for %s — try again", id)
		}
		return err
	}
	if err := app.SaveArtifact(ctx.repoDir, ctx.projDir, id, name, []byte(mermaid+"\n")); err != nil {
		return err
	}
	fmt.Fprintf(out, "sequence diagram for %s drafted and sent (artifact %s) — run 'cerne pull' to review it.\n", id, name)
	return nil
}
