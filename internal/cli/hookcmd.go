package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/hooks"
)

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Install cerne's Claude Code hooks into a project",
	}
	cmd.AddCommand(newHookInstallCmd())
	cmd.AddCommand(newHookBindWorkCmd())
	cmd.AddCommand(newHookWorkGateCmd())
	cmd.AddCommand(newHookLifecycleReminderCmd())
	return cmd
}

func newHookInstallCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install/update cerne's Claude Code hooks (script under .claude/hooks/ + settings.json wiring)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookInstall(cmd, dir)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	return cmd
}

// hookStatusLabel maps an install outcome to its label.
var hookStatusLabel = map[hooks.Status]string{
	hooks.StatusCreated:   "created",
	hooks.StatusUpdated:   "updated",
	hooks.StatusUnchanged: "up to date",
}

func runHookInstall(cmd *cobra.Command, dir string) error {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}
	results, err := hooks.Install(dir)
	if err != nil {
		return err
	}
	printHookResults(cmd.OutOrStdout(), results)
	return nil
}

// printHookResults writes one status line per installed hook asset.
func printHookResults(out io.Writer, results []hooks.Result) {
	for _, r := range results {
		label := hookStatusLabel[r.Status]
		if label == "" {
			label = string(r.Status)
		}
		fmt.Fprintf(out, "%s: %s\n", r.Asset, label)
	}
}
