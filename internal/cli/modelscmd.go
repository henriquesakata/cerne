package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/models"
	"gitlab.com/rockt/cerne/internal/project"
)

func newModelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Manage the user-level model catalog",
	}
	cmd.AddCommand(newModelsRefreshCmd())
	return cmd
}

// newModelsRefreshCmd is the explicit face of the catalog refresh the ui runs
// at boot (F-0311): same feeder, plus --force to skip the 24h window — also the
// diagnosis path when the chat menu does not show an expected model.
func newModelsRefreshCmd() *cobra.Command {
	var dir string
	var force bool
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Refresh the model catalog from the preferred provider's listing",
		Long: "Queries the preferred provider (else the only one the project's [llm] reaches) for its models and " +
			"folds the listing into the global catalog: new models enter as current, vanished ones are delisted, " +
			"nothing is removed. Skipped while the catalog is fresher than 24h unless --force.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelsRefresh(cmd, dir, force)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory whose [llm] config reaches the provider (defaults to cwd)")
	cmd.Flags().BoolVar(&force, "force", false, "query the provider even when the catalog is fresh")
	return cmd
}

func runModelsRefresh(cmd *cobra.Command, dir string, force bool) error {
	out := cmd.OutOrStdout()
	if dir == "" {
		dir = "."
	}
	configDir, err := project.ConfigDir()
	if err != nil {
		return err
	}
	now := time.Now()
	if cat, _ := models.Load(configDir); !force && models.Fresh(cat, now) {
		fmt.Fprintf(out, "catalog is fresh (refreshed %s ago); not querying the provider — use --force to refresh anyway\n",
			now.Sub(cat.UpdatedAt).Round(time.Minute))
		return nil
	}
	diff, err := app.RefreshModels(cmd.Context(), dir, now)
	if errors.Is(err, app.ErrNoModelProvider) {
		fmt.Fprintln(out, "no provider configured in [llm]; nothing to query — the curated baseline stays authoritative")
		return nil
	}
	if err != nil {
		return fmt.Errorf("models refresh failed, catalog unchanged: %w", err)
	}
	fmt.Fprintf(out, "catalog refreshed: %d added, %d delisted, %d relisted\n", len(diff.Added), len(diff.Delisted), len(diff.Relisted))
	for _, id := range diff.Added {
		fmt.Fprintf(out, "  + %s\n", id)
	}
	for _, id := range diff.Delisted {
		fmt.Fprintf(out, "  - %s\n", id)
	}
	for _, id := range diff.Relisted {
		fmt.Fprintf(out, "  ~ %s (relisted)\n", id)
	}
	return nil
}
