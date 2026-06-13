package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/slug"
	"gitlab.com/rockt/cerne/internal/store"
)

// ErrNotMermaidSequence is returned by sanitizeMermaid when the cleaned model
// output is not a Mermaid sequence diagram (does not start with the
// `sequenceDiagram` keyword).
var ErrNotMermaidSequence = errors.New("a saída do modelo não é um diagrama de sequência Mermaid")

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
	name := mermaidArtifactName(rawName)
	spec := readSpecCurrent(ctx.repoDir, id)
	prompt := BuildSequenceDiagramPrompt(f, spec)
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
	mermaid, err := sanitizeMermaid(body)
	if err != nil {
		if errors.Is(err, ErrNotMermaidSequence) {
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

// mermaidArtifactName normalizes a user-supplied --name into a single
// <slug>.mmd filename: it drops any .mmd suffix, slugs the rest (lowercase,
// spaces to "-", diacritics folded), defaults to "sequence" when empty, and
// appends a single .mmd suffix.
func mermaidArtifactName(name string) string {
	base := strings.TrimSuffix(strings.TrimSpace(name), ".mmd")
	base = slug.Make(base)
	if base == "" {
		base = "sequence"
	}
	return base + ".mmd"
}

// BuildSequenceDiagramPrompt assembles the pt-BR prompt instructing the LLM to
// produce a Mermaid sequence diagram for the feature, using its id/title/module
// and the current spec as context. The model is told to answer with bare
// Mermaid (no markdown fences, no prose).
func BuildSequenceDiagramPrompt(f store.Feature, specContent string) string {
	module := f.Module
	if module == "" {
		module = "(sem módulo)"
	}
	var b strings.Builder
	b.WriteString("Você está desenhando um diagrama de sequência (em Mermaid) para uma feature de software, em português do Brasil.\n\n")
	b.WriteString("Feature:\n")
	fmt.Fprintf(&b, "- id: %s\n", f.ID)
	fmt.Fprintf(&b, "- título: %s\n", f.Title)
	fmt.Fprintf(&b, "- módulo: %s\n\n", module)
	b.WriteString("Spec da feature (contexto; pode estar vazia):\n")
	b.WriteString("---\n")
	b.WriteString(strings.TrimSpace(specContent))
	b.WriteString("\n---\n\n")
	b.WriteString("Produza um diagrama de sequência Mermaid (bloco começando com `sequenceDiagram`) que represente o fluxo principal desta feature, usando o título e o módulo como contexto.\n")
	b.WriteString("Responda APENAS com o código Mermaid, sem cercas de markdown e sem texto antes ou depois.\n")
	return b.String()
}

// sanitizeMermaid cleans raw LLM output into a bare Mermaid sequence diagram:
// it strips a surrounding ```mermaid ... ``` (or bare ``` ... ```) fenced block
// when present, trims whitespace, and requires the result to start with
// `sequenceDiagram`. Otherwise it returns ErrNotMermaidSequence.
func sanitizeMermaid(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if inner, ok := extractFencedBlock(s); ok {
		s = strings.TrimSpace(inner)
	}
	if !strings.HasPrefix(s, "sequenceDiagram") {
		return "", ErrNotMermaidSequence
	}
	return s, nil
}

// extractFencedBlock returns the inner content of the first ``` fenced block in
// s (the opening fence may carry an info string such as "mermaid"), reporting
// whether such a block was found.
func extractFencedBlock(s string) (string, bool) {
	const fence = "```"
	open := strings.Index(s, fence)
	if open < 0 {
		return "", false
	}
	// Skip the opening fence and its optional info string up to the newline.
	rest := s[open+len(fence):]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return "", false
	}
	body := rest[nl+1:]
	close := strings.Index(body, fence)
	if close < 0 {
		return "", false
	}
	return body[:close], true
}
