package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/verbs"
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
		Long: "Registers an entity that existed and was absorbed/discontinued before " +
			"it had its own spec/case (e.g. a legacy module). It is born superseded; the " +
			"declared aliases start being flagged by lint in current documents.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRetire(cmd, dir, args[0], by, reason, aliases, importedFrom)
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "successor that absorbed the entity (cerne ID or label, e.g. Finance)")
	cmd.Flags().StringVar(&reason, "reason", "", "reason for the supersession/absorption")
	cmd.Flags().StringArrayVar(&aliases, "alias", nil, fmt.Sprintf("original identifier to flag (repeatable, minimum %d characters)", domain.MinAliasLen))
	cmd.Flags().StringVar(&importedFrom, "imported-from", "", "optional provenance (path/id in the legacy tree)")
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return verbs.MarkLifecycle(cmd)
}

func runRetire(cmd *cobra.Command, dir, name, by, reason string, aliases []string, importedFrom string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("the historical entity name cannot be empty")
	}
	if by == "" {
		return fmt.Errorf("--by is required (cerne ID or label of the successor)")
	}
	if reason == "" {
		return fmt.Errorf("--reason is required")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	h, created, err := app.RecordHistorical(ctx.repoDir, ctx.projDir, name, by, reason, aliases, importedFrom)
	if err != nil {
		if errors.Is(err, domain.ErrAliasTooShort) {
			return fmt.Errorf("alias too short: minimum %d characters", domain.MinAliasLen)
		}
		return err
	}
	if created {
		fmt.Fprintf(cmd.OutOrStdout(), "%s registered as historical (superseded by %s).\n", h.ID, by)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s updated (already existed as historical, superseded by %s).\n", h.ID, by)
	}
	return nil
}

func newForgetAliasCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "forget-alias <name> <alias>...",
		Short: "Remove aliases from a historical entity (curate noisy/ambiguous aliases)",
		Long: "Removes one or more aliases from an already-registered historical entity. " +
			"Use when an alias is a common/ambiguous word (e.g. \"bills\", \"investments\") " +
			"and causes a ref-to-superseded false positive in lint on current documents. " +
			"Missing aliases are ignored; leaving the entity with no aliases is allowed " +
			"(it then matches by id only).",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runForgetAlias(cmd, dir, args[0], args[1:])
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	return cmd
}

func runForgetAlias(cmd *cobra.Command, dir, name string, aliases []string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("the historical entity name cannot be empty")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	h, err := app.ForgetHistoricalAliases(ctx.repoDir, ctx.projDir, name, aliases)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s updated: aliases now [%s].\n", h.ID, strings.Join(h.Aliases, ", "))
	return nil
}
