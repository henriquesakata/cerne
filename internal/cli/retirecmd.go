package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
)

func newRetireCmd() *cobra.Command {
	var (
		dir          string
		by           string
		reason       string
		aliases      []string
		importedFrom string
	)
	cmd := &cobra.Command{
		Use:   "retire <name>",
		Short: "Register a historical entity superseded before it had its own case",
		Long: "Registra uma entidade que existiu e foi absorvida/descontinuada antes " +
			"de ter spec/case próprio (ex.: um módulo legado). Nasce supersedida; os " +
			"aliases declarados passam a ser flagrados pelo lint em documentos vigentes.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRetire(cmd, dir, args[0], by, reason, aliases, importedFrom)
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "sucessor que absorveu a entidade (ID cerne ou rótulo, ex.: Finance)")
	cmd.Flags().StringVar(&reason, "reason", "", "motivo da supersedência/absorção")
	cmd.Flags().StringArrayVar(&aliases, "alias", nil, fmt.Sprintf("identificador original a flagrar (repetível, mínimo %d caracteres)", domain.MinAliasLen))
	cmd.Flags().StringVar(&importedFrom, "imported-from", "", "proveniência opcional (caminho/id na árvore legada)")
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

func runRetire(cmd *cobra.Command, dir, name, by, reason string, aliases []string, importedFrom string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("o nome da entidade histórica não pode ser vazio")
	}
	if by == "" {
		return fmt.Errorf("--by é obrigatório (ID cerne ou rótulo do sucessor)")
	}
	if reason == "" {
		return fmt.Errorf("--reason é obrigatório")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	h, created, err := app.RecordHistorical(ctx.repoDir, ctx.projDir, name, by, reason, aliases, importedFrom)
	if err != nil {
		if errors.Is(err, domain.ErrAliasTooShort) {
			return fmt.Errorf("alias muito curto: mínimo %d caracteres", domain.MinAliasLen)
		}
		return err
	}
	if created {
		fmt.Fprintf(cmd.OutOrStdout(), "%s registrada como histórica (supersedida por %s).\n", h.ID, by)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s atualizada (já existia como histórica, supersedida por %s).\n", h.ID, by)
	}
	return nil
}
