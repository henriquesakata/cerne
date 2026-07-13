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
	cmd.AddCommand(newHookStopCheckCmd())
	cmd.AddCommand(newHookProvenanceCmd())
	cmd.AddCommand(newHookCommitTrailerCmd())
	cmd.AddCommand(newHookSessionContextCmd())
	return cmd
}

func newHookInstallCmd() *cobra.Command {
	var dir string
	var providers []string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install/update cerne's agent hooks (Claude Code natively; Cursor/Codex/Gemini adapters — F-0174)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHookInstall(cmd, dir, providers)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	cmd.Flags().StringSliceVar(&providers, "provider", nil, "providers to install (claude|cursor|codex|gemini); default: claude plus any auto-detected by config dir")
	return cmd
}

// hookStatusLabel maps an install outcome to its label.
var hookStatusLabel = map[hooks.Status]string{
	hooks.StatusCreated:   "created",
	hooks.StatusUpdated:   "updated",
	hooks.StatusUnchanged: "up to date",
	hooks.StatusSkipped:   "skipped",
}

func runHookInstall(cmd *cobra.Command, dir string, providerNames []string) error {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}
	var ps []hooks.Provider
	if len(providerNames) == 0 {
		// Default: Claude Code plus every provider whose config dir exists.
		ps = append([]hooks.Provider{hooks.ProviderClaude}, hooks.DetectProviders(dir)...)
	} else {
		for _, name := range providerNames {
			ps = append(ps, hooks.Provider(name))
		}
	}
	results, err := hooks.Install(dir, ps...)
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
		if r.Note != "" {
			fmt.Fprintf(out, "%s: %s — %s\n", r.Asset, label, r.Note)
			continue
		}
		fmt.Fprintf(out, "%s: %s\n", r.Asset, label)
	}
}
