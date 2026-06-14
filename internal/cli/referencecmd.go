package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
)

func newReferenceCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "reference",
		Short: "Register and manage references in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newReferenceNewCmd(&dir))
	return cmd
}

func newReferenceNewCmd(dir *string) *cobra.Command {
	var url, informs, file string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Register an external source (article/doc) as a reference",
		Long: "Registra um artigo/documento externo como referência (marca externa + URL). " +
			"O conteúdo (texto/resumo) vem de --file ou da entrada padrão e entra no índice, " +
			"então o 'ask' passa a citá-lo. Use --informs para ligar a fonte às features/decisões " +
			"(F-/D-) que ela motivou.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReferenceNew(cmd, *dir, args[0], url, informs, file)
		},
	}
	cmd.Flags().StringVar(&url, "url", "", "source URL (required)")
	cmd.Flags().StringVar(&informs, "informs", "", "comma-separated F-/D- ids this source motivated")
	cmd.Flags().StringVar(&file, "file", "", "file with the source content (defaults to stdin)")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}

func runReferenceNew(cmd *cobra.Command, dir, title, url, informs, file string) error {
	content, err := readReferenceContent(cmd, file)
	if err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	r, err := app.CreateExternalReference(ctx.repoDir, ctx.projDir, title, url, content, splitInforms(informs))
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s registrada (externa: %s).\n", r.ID, r.URL)
	return nil
}

// readReferenceContent reads the source content from --file, or from stdin when
// no file is given.
func readReferenceContent(cmd *cobra.Command, file string) (string, error) {
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("--file: %w", err)
		}
		return string(data), nil
	}
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// splitInforms parses the comma-separated --informs flag into trimmed ids
// (empties dropped); app.CreateExternalReference validates them.
func splitInforms(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(csv, ",") {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
